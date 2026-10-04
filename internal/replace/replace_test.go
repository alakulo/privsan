package replace

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"privsan/internal/model"
)

func TestReplacementSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, input, find, with, want string
		regex, ignore                 bool
	}{
		{"literal", "a.b aXb", "a.b", "$1\\n", "$1\\n aXb", false, false},
		{"unicode", "中文猫\r\n猫", "猫", "狗", "中文狗\r\n狗", false, false},
		{"delete", "aaaaa", "aa", "", "a", false, false},
		{"multiline", "a\nb\r\na\nb", "a\nb", "x\ny", "x\ny\r\nx\ny", false, false},
		{"case", "Ä ä A a", "ä", "yes", "yes yes A a", false, true},
		{"regex", "build-123 build-45", `build-\d+`, "$1", "$1 $1", true, false},
		{"no-match", "ordinary", "absent", "new", "ordinary", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Compile(Options{Find: tc.find, With: tc.with, Regex: tc.regex, IgnoreCase: tc.ignore})
			if err != nil {
				t.Fatal(err)
			}
			hits, err := p.Find(context.Background(), []byte(tc.input), 100, 1024)
			if err != nil {
				t.Fatal(err)
			}
			out, err := model.Render([]byte(tc.input), hits, false)
			if err != nil || string(out) != tc.want {
				t.Fatalf("%q %v", out, err)
			}
			if tc.name == "unicode" && (hits[0].Column != 3 || hits[1].Line != 2 || hits[1].Column != 1 || hits[0].Start != 6) {
				t.Fatal(hits)
			}
		})
	}
}

func TestValidationBudgetsAndCancellation(t *testing.T) {
	for _, opts := range []Options{{}, {Find: "secret(", Regex: true}, {Find: "a*", Regex: true}, {Find: "(?=a)", Regex: true}, {Find: "\x00"}, {Find: "\xff"}, {Find: strings.Repeat("a", 4097)}, {Find: "a", With: strings.Repeat("b", 4097)}} {
		if _, err := Compile(opts); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("validation: %+v %v", opts, err)
		}
	}
	p, _ := Compile(Options{Find: "a", With: "bbbb"})
	if _, err := p.Find(context.Background(), []byte("aa"), 1, 100); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if _, err := p.Find(context.Background(), []byte("aa"), 2, 7); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if _, err := p.Find(context.Background(), []byte("aa"), 2, 8); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Find(ctx, []byte("a"), 1, 100); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	p, _ = Compile(Options{Find: `\b`, Regex: true})
	if _, err := p.Find(context.Background(), []byte("abc"), 100, 100); !errors.Is(err, ErrZeroWidth) {
		t.Fatal(err)
	}
	// Shrinking matches must not offset the expansion budget for a subset.
	p, _ = Compile(Options{Find: "longword|x", With: "zz", Regex: true})
	if _, err := p.Find(context.Background(), []byte("longword x"), 10, 10); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}

func BenchmarkFind16MiB(b *testing.B) {
	block := append([]byte("TOKEN "), bytes.Repeat([]byte("ordinary "), 113)...)
	block = append(block, '\n') // 1024 bytes
	data := bytes.Repeat(block, (16<<20)/len(block))
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"literal", Options{Find: "TOKEN", With: "VALUE"}},
		{"literal-no-match", Options{Find: "absent", With: "VALUE"}},
		{"regex", Options{Find: `TO[K]EN`, With: "VALUE", Regex: true}},
		{"ignore-case", Options{Find: "token", With: "VALUE", IgnoreCase: true}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			p, err := Compile(tc.opts)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := p.Find(context.Background(), data, 100000, 16<<20); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
