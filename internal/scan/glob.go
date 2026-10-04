package scan

import (
	"fmt"
	"path"
	"strings"
)

// Glob implements a small, documented slash-based pattern language.
type Glob struct{ parts []string }

func CompileGlob(pattern string) (Glob, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "\\") || strings.Contains(pattern, "!") || len(pattern) > 512 {
		return Glob{}, fmt.Errorf("invalid path pattern")
	}
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	parts := strings.Split(pattern, "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return Glob{}, fmt.Errorf("invalid path pattern segment")
		}
		if p != "**" {
			if _, err := path.Match(p, ""); err != nil {
				return Glob{}, fmt.Errorf("invalid glob")
			}
		}
	}
	return Glob{parts}, nil
}
func (g Glob) Match(name string) bool {
	segments := strings.Split(name, "/")
	type key struct{ i, j int }
	memo := map[key]bool{}
	seen := map[key]bool{}
	var match func(i, j int) bool
	match = func(i, j int) bool {
		k := key{i, j}
		if seen[k] {
			return memo[k]
		}
		seen[k] = true
		result := false
		if i == len(g.parts) {
			result = j == len(segments)
		} else if g.parts[i] == "**" {
			result = match(i+1, j) || (j < len(segments) && match(i, j+1))
		} else if j < len(segments) {
			ok, _ := path.Match(g.parts[i], segments[j])
			result = ok && match(i+1, j+1)
		}
		memo[k] = result
		return result
	}
	return match(0, 0)
}
