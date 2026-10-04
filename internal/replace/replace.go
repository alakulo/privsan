// Package replace plans bounded, non-overlapping text replacements. It never
// writes files and does not include search text in errors or reports.
package replace

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"unicode/utf8"

	"privsan/internal/model"
)

var ErrLimit = errors.New("replacement finding or output budget exceeded")
var ErrZeroWidth = errors.New("zero-width replacement matches are not supported")

type Options struct {
	Find, With        string
	Regex, IgnoreCase bool
}

// Plan is immutable after compilation and safe for concurrent file workers.
type Plan struct {
	options Options
	needle  []byte
	re      *regexp.Regexp
}

func Compile(opts Options) (*Plan, error) {
	if opts.Find == "" || len(opts.Find) > 4096 || len(opts.With) > 4096 ||
		!utf8.ValidString(opts.Find) || !utf8.ValidString(opts.With) ||
		bytes.IndexByte([]byte(opts.Find), 0) >= 0 || bytes.IndexByte([]byte(opts.With), 0) >= 0 {
		return nil, errors.New("find must be nonempty UTF-8; find and replacement must be NUL-free and at most 4096 bytes")
	}
	p := &Plan{options: opts, needle: []byte(opts.Find)}
	if opts.Regex || opts.IgnoreCase {
		pattern := opts.Find
		if !opts.Regex {
			pattern = regexp.QuoteMeta(pattern)
		}
		if opts.IgnoreCase {
			pattern = "(?i:" + pattern + ")"
		}
		var err error
		p.re, err = regexp.Compile(pattern)
		if err != nil {
			return nil, errors.New("invalid Go regular expression")
		}
		if p.re.MatchString("") {
			return nil, ErrZeroWidth
		}
	}
	return p, nil
}

func (p *Plan) Options() Options { return p.options }

// Find uses byte offsets and advances line/Unicode-column bookkeeping once.
// The output budget reserves every positive expansion, so any selection subset
// is safe, including mixtures of deletions and expanding replacements.
func (p *Plan) Find(ctx context.Context, data []byte, limit int, maxOutput int64) ([]model.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || int64(len(data)) > maxOutput {
		return nil, ErrLimit
	}
	var spans [][]int
	if p.re != nil {
		spans = p.re.FindAllIndex(data, limit+1)
		if len(spans) > limit {
			return nil, ErrLimit
		}
	}
	var hits []model.Finding
	pos, line, column, start := 0, 1, 1, 0
	budget := int64(len(data))
	for i := 0; ; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a, b := 0, 0
		if p.re != nil {
			if i == len(spans) {
				break
			}
			a, b = spans[i][0], spans[i][1]
		} else {
			at := bytes.Index(data[start:], p.needle)
			if at < 0 {
				break
			}
			a = start + at
			b = a + len(p.needle)
		}
		if a == b {
			return nil, ErrZeroWidth
		}
		if hits == nil {
			hits = make([]model.Finding, 0, min(limit, 256))
		}
		if len(hits) == limit {
			return nil, ErrLimit
		}
		budget += int64(max(0, len(p.options.With)-(b-a)))
		if budget > maxOutput {
			return nil, ErrLimit
		}
		for pos < a {
			if data[pos] == '\n' {
				line++
				column = 1
				pos++
			} else {
				_, n := utf8.DecodeRune(data[pos:])
				pos += n
				column++
			}
		}
		hits = append(hits, model.Finding{ID: len(hits) + 1, Rule: "replace", Start: a, End: b,
			Line: line, Column: column, Replacement: p.options.With, Selected: true})
		start = b
	}
	return hits, nil
}
