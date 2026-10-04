package detect

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"privsan/internal/model"
	"privsan/internal/policy"
)

var ErrLimit = errors.New("finding limit exceeded")
var ErrZeroWidth = errors.New("zero-width rule match")

type candidate struct{ start, end, priority, rule int }

func digit(b byte) bool { return b >= '0' && b <= '9' }
func StrictID(s string) bool {
	if len(s) != 18 {
		return false
	}
	if s[:6] == "000000" {
		return false
	}
	y, e := strconv.Atoi(s[6:10])
	if e != nil || y < 1800 || y > time.Now().Year() {
		return false
	}
	if _, e = time.Parse("20060102", s[6:14]); e != nil {
		return false
	}
	if s[14:17] == "000" {
		return false
	}
	weights := []int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	sum := 0
	for i, w := range weights {
		if !digit(s[i]) {
			return false
		}
		sum += int(s[i]-'0') * w
	}
	return strings.ToUpper(s[17:]) == string("10X98765432"[sum%11])
}
func Find(ctx context.Context, data []byte, p *policy.Policy, limit int) ([]model.Finding, error) {
	if limit < 1 {
		return nil, ErrLimit
	}
	cs := make([]candidate, 0)
	// Bound candidates as well as accepted results: overlapping rules must not
	// multiply memory without limit.
	capCandidates := min(limit*4, 2000000)
	for ri, r := range p.Rules {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		matches := r.RE.FindAllIndex(data, capCandidates+1)
		if len(matches) > capCandidates || len(cs)+len(matches) > capCandidates {
			return nil, ErrLimit
		}
		for _, m := range matches {
			a, b := m[0], m[1]
			if a == b {
				return nil, ErrZeroWidth
			}
			if r.Builtin {
				if r.ID == "phone" || r.ID == "id" || r.ID == "ipv4" {
					if a > 0 && digit(data[a-1]) || b < len(data) && digit(data[b]) {
						continue
					}
				}
				if r.ID == "id" && p.Config.IDValidation == "strict" && !StrictID(string(data[a:b])) {
					continue
				}
				if r.ID == "ipv4" || r.ID == "ipv6" {
					if r.ID == "ipv6" {
						for b > a && data[b-1] == '.' {
							b--
						}
					}
					ip, err := netip.ParseAddr(string(data[a:b]))
					if err != nil || r.ID == "ipv4" && !ip.Is4() || r.ID == "ipv6" && !ip.Is6() {
						continue
					}
					if r.ID == "ipv4" && (a > 1 && data[a-1] == '.' && digit(data[a-2]) || b+1 < len(data) && data[b] == '.' && digit(data[b+1])) {
						continue
					}
				}
			}
			cs = append(cs, candidate{a, b, r.Priority, ri})
		}
	}
	sort.Slice(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.start != b.start {
			return a.start < b.start
		}
		if a.end != b.end {
			return a.end > b.end
		}
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		return p.Rules[a.rule].ID < p.Rules[b.rule].ID
	})
	// Merge intersecting spans so competing rules cannot leave an uncovered
	// suffix. The deterministic first candidate supplies the label/mask.
	merged := make([]candidate, 0, len(cs))
	for _, c := range cs {
		if len(merged) > 0 && c.start < merged[len(merged)-1].end {
			merged[len(merged)-1].end = max(merged[len(merged)-1].end, c.end)
		} else {
			merged = append(merged, c)
		}
	}
	hits := []model.Finding{}
	end := 0
	line, col, pos := 1, 1, 0
	for _, c := range merged {
		if c.start < end {
			continue
		}
		if len(hits) >= limit {
			return nil, ErrLimit
		}
		// Advance each input byte at most once while computing Unicode columns.
		for pos < c.start {
			if data[pos] == '\n' {
				line++
				col = 1
				pos++
			} else {
				_, n := utf8.DecodeRune(data[pos:c.start])
				pos += n
				col++
			}
		}
		r := p.Rules[c.rule]
		hits = append(hits, model.Finding{ID: len(hits) + 1, Rule: r.ID, Start: c.start, End: c.end, Line: line, Column: col, Replacement: p.Replacement(r, data[c.start:c.end]), Selected: true})
		end = c.end
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}
func CSVSafe(data []byte, hits []model.Finding) bool {
	for _, h := range hits {
		if bytes.ContainsAny(data[h.Start:h.End], ",;\"\r\n") {
			return false
		}
	}
	return true
}
