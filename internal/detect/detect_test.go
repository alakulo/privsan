package detect

import (
	"context"
	"privsan/internal/model"
	"privsan/internal/policy"
	"strings"
	"testing"
	"unicode/utf8"
)

func testPolicy(t testing.TB) *policy.Policy {
	t.Helper()
	p, e := policy.Compile(policy.Default())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestBuiltinRedaction(t *testing.T) {
	p := testPolicy(t)
	cases := []struct{ input, want string }{
		{"手机 +86 138-0013-8000 / 138 0013 8000", "手机 [PHONE] / [PHONE]"},
		{"13800138000@example.com x+y@a-b.example", "[EMAIL] [EMAIL]"},
		{"110101199003071234", "[ID]"},
		{"IP 192.0.2.1 ::1 :: 2001:db8::1. ::ffff:192.0.2.1 fe80::1%eth0", "IP [IP] [IP] [IP] [IP]. [IP] [IP]"},
		{"bad 999.1.2.3 1.2.3.4.5 113800138000", "bad 999.1.2.3 1.2.3.4.5 113800138000"},
		{"\ufeff中文 13800138000\r\nalice@example.com\n", "\ufeff中文 [PHONE]\r\n[EMAIL]\n"},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			h, e := Find(context.Background(), []byte(tt.input), p, 100)
			if e != nil {
				t.Fatal(e)
			}
			b, e := model.Render([]byte(tt.input), h, false)
			if e != nil || string(b) != tt.want {
				t.Fatalf("got %q (%v), want %q", b, e, tt.want)
			}
		})
	}
}
func TestStrictID(t *testing.T) {
	if !StrictID("11010519491231002X") {
		t.Fatal("valid checksum rejected")
	}
	for _, s := range []string{"110105194912310020", "11010519490230002X", "00000019491231002X", "11010519491231000X", "1"} {
		if StrictID(s) {
			t.Fatalf("accepted %q", s)
		}
	}
	c := policy.Default()
	c.IDValidation = "strict"
	p, _ := policy.Compile(c)
	h, e := Find(context.Background(), []byte("11010519491231002X 110101199003071234"), p, 10)
	if e != nil || len(h) != 1 || h[0].Rule != "id" {
		t.Fatalf("%+v %v", h, e)
	}
}
func TestLimitsCancelPositionsAndSelection(t *testing.T) {
	p := testPolicy(t)
	data := []byte("中文 13800138000\nx a@example.com")
	h, e := Find(context.Background(), data, p, 10)
	if e != nil {
		t.Fatal(e)
	}
	if h[0].Line != 1 || h[0].Column != 4 || h[0].Start != 7 || h[1].Line != 2 || h[1].Column != 3 {
		t.Fatalf("%+v", h)
	}
	h[0].Selected = false
	b, _ := model.Render(data, h, false)
	if !strings.Contains(string(b), "13800138000") {
		t.Fatal("selection ignored")
	}
	b, _ = model.Render(data, h, true)
	if strings.Contains(string(b), "13800138000") {
		t.Fatal("safe preview leaked")
	}
	if _, e = Find(context.Background(), data, p, 1); e != ErrLimit {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = Find(ctx, data, p, 10); e != context.Canceled {
		t.Fatal(e)
	}
}
func TestOverlapsAndZeroWidth(t *testing.T) {
	c := policy.Default()
	c.Rules = []policy.Rule{{ID: "credential", Pattern: "token=abcdef", Replacement: "[TOKEN]", Priority: 200}, {ID: "tail", Pattern: "abcdefSECRET", Replacement: "[SECRET]", Priority: 100}}
	p, e := policy.Compile(c)
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("token=abcdefSECRET")
	h, e := Find(context.Background(), data, p, 100)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := model.Render(data, h, false)
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("overlap left an exposed suffix")
	}
	c.Rules = []policy.Rule{{ID: "boundary", Pattern: "\\b", Replacement: "X"}}
	p, e = policy.Compile(c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Find(context.Background(), []byte("word"), p, 10); e != ErrZeroWidth {
		t.Fatal(e)
	}
}
func TestCSVStructure(t *testing.T) {
	c := policy.Default()
	c.Rules = []policy.Rule{{ID: "cross", Pattern: "alice,secret", Replacement: "X"}}
	p, _ := policy.Compile(c)
	d := []byte("alice,secret")
	h, e := Find(context.Background(), d, p, 10)
	if e != nil {
		t.Fatal(e)
	}
	if CSVSafe(d, h) {
		t.Fatal("unsafe CSV accepted")
	}
}
func FuzzFind(f *testing.F) {
	f.Add("中文 a@example.com\r\n13800138000")
	f.Add("::ffff:192.0.2.1")
	f.Add("")
	p := testPolicy(f)
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 || !utf8.ValidString(s) {
			t.Skip()
		}
		h, e := Find(context.Background(), []byte(s), p, 1000)
		if e != nil {
			return
		}
		if _, e = model.Render([]byte(s), h, false); e != nil {
			t.Fatal(e)
		}
		last := 0
		for _, v := range h {
			if v.Start < last || v.End <= v.Start || v.End > len(s) {
				t.Fatal("invalid span")
			}
			last = v.End
		}
	})
}
func BenchmarkFind(b *testing.B) {
	p := testPolicy(b)
	data := []byte(strings.Repeat("normal application event user=alice@example.com source=192.0.2.1 phone=13800138000\n", 1000))
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, e := Find(context.Background(), data, p, 10000); e != nil {
			b.Fatal(e)
		}
	}
}
