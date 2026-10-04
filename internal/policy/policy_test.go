package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictConfig(t *testing.T) {
	for _, data := range []string{
		`{"version":1,"version":1}`,
		`{"version":1,"limits":{"workers":1,"workers":2}}`,
		`{"version":1,"unknown":true}`,
		`{"version":1} {}`,
		`null`, `[]`, `{}`,
	} {
		t.Run(data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy.json")
			os.WriteFile(path, []byte(data), 0600)
			if _, e := Load(path); e == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	os.WriteFile(path, []byte("\ufeff"+`{"version":1,"limits":{"workers":2}}`), 0600)
	c, e := Load(path)
	if e != nil || c.Limits.Workers != 2 || c.Limits.MaxFile != 16<<20 {
		t.Fatalf("%+v %v", c, e)
	}
}
func TestCompileRejectsUnsafeRules(t *testing.T) {
	cases := []Rule{
		{ID: "phone", Pattern: "x", Replacement: "X"},
		{ID: "wrong name", Pattern: "x", Replacement: "X"},
		{ID: "custom", Pattern: "[", Replacement: "X"},
		{ID: "custom", Pattern: ".*", Replacement: "X"},
		{ID: "custom", Pattern: "x", Replacement: "\x1b[2J"},
		{ID: "custom", Pattern: "x", Replacement: "a,b"},
	}
	for _, r := range cases {
		c := Default()
		c.Rules = []Rule{r}
		if _, e := Compile(c); e == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
	c := Default()
	c.Disable = []string{"bad"}
	if _, e := Compile(c); e == nil {
		t.Fatal("unknown rule accepted")
	}
	c = Default()
	c.Limits.Workers = 0
	if _, e := Compile(c); e == nil {
		t.Fatal("zero workers accepted")
	}
}
func TestHMAC(t *testing.T) {
	t.Setenv("PRIVSAN_HMAC_KEY", strings.Repeat("k", 32))
	c := Default()
	c.Strategy = "hmac"
	p, e := Compile(c)
	if e != nil {
		t.Fatal(e)
	}
	r := p.Rules[0]
	first := p.Replacement(r, []byte("secret"))
	if first != p.Replacement(r, []byte("secret")) {
		t.Fatal("unstable")
	}
	if first == p.Replacement(p.Rules[1], []byte("secret")) {
		t.Fatal("missing type separation")
	}
	if strings.Contains(first, "secret") {
		t.Fatal("plaintext leaked")
	}
	t.Setenv("PRIVSAN_HMAC_KEY", strings.Repeat("z", 32))
	other, e := Compile(c)
	if e != nil {
		t.Fatal(e)
	}
	if first == other.Replacement(r, []byte("secret")) {
		t.Fatal("key ignored")
	}
	t.Setenv("PRIVSAN_HMAC_KEY", "short")
	if _, e := Compile(c); e == nil {
		t.Fatal("weak key accepted")
	}
}
