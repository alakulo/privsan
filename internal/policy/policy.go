package policy

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"

	"privsan/internal/model"
)

type Limits struct {
	MaxFile     int64 `json:"max_file_bytes"`
	MaxTotal    int64 `json:"max_total_bytes"`
	MaxFiles    int   `json:"max_files"`
	MaxFindings int   `json:"max_findings"`
	Workers     int   `json:"workers"`
}
type Rule struct {
	ID          string `json:"id"`
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Priority    int    `json:"priority"`
}
type Config struct {
	Version      int      `json:"version"`
	Disable      []string `json:"disable"`
	IDValidation string   `json:"id_validation"`
	Strategy     string   `json:"strategy"`
	HMACKeyEnv   string   `json:"hmac_key_env"`
	Limits       Limits   `json:"limits"`
	Include      []string `json:"include"`
	Exclude      []string `json:"exclude"`
	Rules        []Rule   `json:"rules"`
}
type CompiledRule struct {
	Rule
	RE      *regexp.Regexp
	Builtin bool
}
type Policy struct {
	Config Config
	Rules  []CompiledRule
	ID     string
	key    []byte
}

func Default() Config {
	return Config{Version: 1, Disable: []string{}, IDValidation: "candidate", Strategy: "placeholder", HMACKeyEnv: "PRIVSAN_HMAC_KEY", Limits: Limits{16 << 20, 128 << 20, 10000, 100000, min(4, runtime.NumCPU())}, Include: []string{}, Exclude: []string{}, Rules: []Rule{}}
}

// StrictJSON rejects duplicate keys at every nesting level as well as unknown fields.
func StrictJSON(data []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		token, err := d.Token()
		if err != nil {
			return err
		}
		if token == nil {
			return errors.New("null values are not supported")
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return errors.New("duplicate or invalid JSON key")
				}
				seen[key] = true
				if e = walk(); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e := walk(); e != nil {
					return e
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := walk(); err != nil {
		return errors.New("invalid or duplicate-key JSON")
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("expected exactly one JSON value")
	}
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return errors.New("expected JSON object")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return errors.New("unknown field or invalid configuration value")
	}
	return nil
}
func Load(path string) (Config, error) {
	c := Default()
	if path == "" {
		return c, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return c, errors.New("cannot open configuration")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return c, errors.New("configuration exceeds limit or cannot be read")
	}
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	if err = StrictJSON(b, &c); err != nil {
		return c, err
	}
	// version is mandatory even though other values have documented defaults.
	var version struct {
		Version *int `json:"version"`
	}
	_ = json.Unmarshal(b, &version)
	if version.Version == nil {
		return c, errors.New("configuration version is required")
	}
	return c, nil
}
func Builtins() []Rule {
	return []Rule{
		{"id", "[0-9]{17}[0-9Xx]", "[ID]", 100},
		{"email", "[A-Za-z0-9.!#$%&'*+/?^_\\x60{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:[.][A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+", "[EMAIL]", 100},
		{"phone", "(?:[+]?86[ -]?)?1[3-9][0-9][ -]?[0-9]{4}[ -]?[0-9]{4}", "[PHONE]", 100},
		{"ipv6", "[0-9A-Fa-f:]*:[0-9A-Fa-f:.]*:[0-9A-Fa-f:.]*(?:%[A-Za-z0-9_.-]+)?", "[IP]", 100},
		{"ipv4", "(?:[0-9]{1,3}[.]){3}[0-9]{1,3}", "[IP]", 100},
	}
}
func Compile(c Config) (*Policy, error) {
	fail := func(s string) (*Policy, error) { return nil, errors.New(s) }
	if c.Version != 1 {
		return fail("unsupported policy version")
	}
	if c.IDValidation != "candidate" && c.IDValidation != "strict" {
		return fail("id_validation must be candidate or strict")
	}
	if c.Strategy != "placeholder" && c.Strategy != "mask" && c.Strategy != "hmac" {
		return fail("strategy must be placeholder, mask or hmac")
	}
	l := c.Limits
	if l.MaxFile < 1 || l.MaxFile > 256<<20 || l.MaxTotal < l.MaxFile || l.MaxTotal > 1<<30 || l.MaxFiles < 1 || l.MaxFiles > 100000 || l.MaxFindings < 1 || l.MaxFindings > 1000000 || l.Workers < 1 || l.Workers > 32 {
		return fail("limits outside supported bounds")
	}
	if len(c.Rules) > 64 || len(c.Include)+len(c.Exclude) > 1024 {
		return fail("too many rules or path patterns")
	}
	p := &Policy{Config: c}
	if c.Strategy == "hmac" {
		if c.HMACKeyEnv == "" {
			return fail("hmac_key_env is required")
		}
		p.key = []byte(os.Getenv(c.HMACKeyEnv))
		if len(p.key) < 32 {
			return fail("HMAC key environment value must contain at least 32 bytes")
		}
	}
	known := map[string]bool{}
	disabled := map[string]bool{}
	for _, r := range Builtins() {
		known[r.ID] = true
	}
	for _, id := range c.Disable {
		if !known[id] || disabled[id] {
			return fail("unknown or repeated disabled rule")
		}
		disabled[id] = true
	}
	specs := []CompiledRule{}
	for _, r := range Builtins() {
		if !disabled[r.ID] {
			specs = append(specs, CompiledRule{Rule: r, Builtin: true})
		}
	}
	validID := regexp.MustCompile("^[a-z][a-z0-9_]{0,31}$")
	for _, r := range c.Rules {
		if !validID.MatchString(r.ID) || known[r.ID] || r.Pattern == "" || len(r.Pattern) > 4096 || r.Replacement == "" || len(r.Replacement) > 128 || r.Priority < -1000 || r.Priority > 1000 {
			return fail("invalid custom rule or duplicate ID")
		}
		// Literal replacements must be safe in common CSV dialects and terminals.
		if strings.ContainsAny(r.Replacement, ",;\"\r\n\x00\x1b") {
			return fail("replacement contains structural CSV or terminal characters")
		}
		for _, v := range r.Replacement {
			if v < 32 || v == 127 {
				return fail("replacement contains control characters")
			}
		}
		known[r.ID] = true
		specs = append(specs, CompiledRule{Rule: r})
	}
	for i := range specs {
		re, err := regexp.Compile(specs[i].Pattern)
		if err != nil || re.MatchString("") {
			return fail(fmt.Sprintf("rule %s has an invalid or empty-matching pattern", specs[i].ID))
		}
		specs[i].RE = re
	}
	p.Rules = specs
	b, _ := json.Marshal(c)
	p.ID = model.Hash(b)
	return p, nil
}
func (p *Policy) Replacement(rule CompiledRule, value []byte) string {
	if !rule.Builtin {
		return rule.Replacement
	}
	switch p.Config.Strategy {
	case "mask":
		return "[REDACTED]"
	case "hmac":
		mac := hmac.New(sha256.New, p.key)
		mac.Write([]byte(rule.ID))
		mac.Write([]byte{0})
		mac.Write(value)
		return "[" + rule.ID + ":" + hex.EncodeToString(mac.Sum(nil)[:16]) + "]"
	default:
		return rule.Replacement
	}
}
