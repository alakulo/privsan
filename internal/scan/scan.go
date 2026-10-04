package scan

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"privsan/internal/detect"
	"privsan/internal/fsutil"
	"privsan/internal/model"
	"privsan/internal/policy"
)

type Options struct {
	Root             string
	Include, Exclude []string
	NoIgnore         bool
}
type Progress struct{ Completed, Total, Findings int }

func Run(ctx context.Context, opts Options, p *policy.Policy, progress func(Progress)) model.Snapshot {
	s := model.Snapshot{Files: []model.File{}, Issues: []model.Issue{}, Skipped: map[string]int{}, PolicyID: p.ID}
	fail := func(code, path, msg string) { s.Issues = append(s.Issues, model.Error(code, path, msg)) }
	if opts.Root == "" {
		opts.Root = "."
	}
	abs, err := fsutil.CanonicalRoot(opts.Root)
	if err != nil {
		fail("E_INPUT", "", "invalid root")
		return s
	}
	if err = fsutil.NoLinks(abs); err != nil {
		fail("E_INPUT", "", "root must exist and not traverse symbolic links")
		return s
	}
	info, err := os.Lstat(abs)
	if err != nil {
		fail("E_INPUT", "", "cannot inspect root")
		return s
	}
	target := "."
	s.Root = abs
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			fail("E_INPUT", "", "input is not a regular file")
			return s
		}
		target = filepath.Base(abs)
		s.Root = filepath.Dir(abs)
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		fail("E_INPUT", "", "cannot open root")
		return s
	}
	defer root.Close()
	inc := []Glob{}
	exc := []Glob{}
	patterns := append(append([]string{}, p.Config.Exclude...), opts.Exclude...)
	if !opts.NoIgnore {
		if st, e := root.Lstat(".privsanignore"); e == nil {
			if !st.Mode().IsRegular() {
				fail("E_INPUT", ".privsanignore", "ignore file must be regular")
				return s
			}
			b, _, e := fsutil.Read(root, ".privsanignore", 65536)
			if e != nil || !utf8.Valid(b) {
				fail("E_INPUT", ".privsanignore", "cannot read UTF-8 ignore file within 64 KiB")
				return s
			}
			for _, line := range strings.Split(strings.TrimPrefix(string(b), "\ufeff"), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					patterns = append(patterns, line)
				}
			}
		} else if !os.IsNotExist(e) {
			fail("E_INPUT", ".privsanignore", "cannot inspect ignore file")
			return s
		}
	}
	if len(patterns) > 1024 {
		fail("E_CONFIG", "", "too many exclude patterns")
		return s
	}
	for _, v := range patterns {
		g, e := CompileGlob(v)
		if e != nil {
			fail("E_CONFIG", "", "invalid exclude pattern")
			return s
		}
		exc = append(exc, g)
	}
	for _, v := range append(append([]string{}, p.Config.Include...), opts.Include...) {
		g, e := CompileGlob(v)
		if e != nil {
			fail("E_CONFIG", "", "invalid include pattern")
			return s
		}
		inc = append(inc, g)
	}
	match := func(gs []Glob, name string) bool {
		for _, g := range gs {
			if g.Match(name) {
				return true
			}
		}
		return false
	}
	type item struct {
		path string
		size int64
	}
	items := []item{}
	var total int64
	stop := errors.New("stop")
	err = boundedWalk(ctx, root, target, p.Config.Limits.MaxFiles*10+1024, func(path string, d fs.DirEntry, e error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e != nil {
			fail("E_READ", path, "cannot enumerate input")
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			s.Skipped["S_SYMLINK"]++
			return nil
		}
		if path != "." && (d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == ".privsan") || match(exc, path)) {
			s.Skipped["S_EXCLUDED"]++
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if len(inc) > 0 && !match(inc, path) {
			s.Skipped["S_EXCLUDED"]++
			return nil
		}
		st, e := d.Info()
		if e != nil {
			fail("E_READ", path, "cannot inspect input")
			return nil
		}
		if !st.Mode().IsRegular() {
			s.Skipped["S_SPECIAL"]++
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".md", ".txt", ".csv", ".log":
		default:
			s.Skipped["S_EXTENSION"]++
			if path == target && target != "." {
				fail("E_INPUT", path, "unsupported file extension")
			}
			return nil
		}
		if len(items) >= p.Config.Limits.MaxFiles {
			fail("E_LIMIT", path, "file count limit exceeded")
			return stop
		}
		if st.Size() > p.Config.Limits.MaxFile {
			fail("E_LIMIT", path, "file size limit exceeded")
			return nil
		}
		total += st.Size()
		if total > p.Config.Limits.MaxTotal {
			fail("E_LIMIT", path, "total input size limit exceeded")
			return stop
		}
		items = append(items, item{path, st.Size()})
		return nil
	})
	if ctx.Err() != nil {
		fail("E_CANCELLED", "", "scan cancelled")
		return s
	}
	if err != nil && !errors.Is(err, stop) {
		if errors.Is(err, errWalkLimit) {
			fail("E_LIMIT", "", "directory entry or depth budget exceeded")
		} else {
			fail("E_READ", "", "enumeration failed")
		}
	}
	// A failed enumeration never begins reading potentially partial scope.
	if len(s.Issues) > 0 {
		return s
	}
	sort.Slice(items, func(i, j int) bool { return items[i].path < items[j].path })
	type result struct {
		file  model.File
		issue *model.Issue
	}
	results := make([]result, len(items))
	work := make(chan int)
	done := make(chan int, p.Config.Limits.Workers)
	var wg sync.WaitGroup
	for w := 0; w < p.Config.Limits.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				it := items[i]
				res := result{}
				set := func(code, msg string) { v := model.Error(code, it.path, msg); res.issue = &v }
				if ctx.Err() != nil {
					set("E_CANCELLED", "scan cancelled")
				} else {
					data, st, e := fsutil.Read(root, it.path, it.size)
					if e != nil {
						set("E_READ", "file unreadable or changed since enumeration")
					} else if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
						set("E_ENCODING", "input must be UTF-8 text without NUL")
					} else {
						hits, e := detect.Find(ctx, data, p, p.Config.Limits.MaxFindings)
						if e != nil {
							code := "E_RULE"
							msg := "rule evaluation failed"
							if errors.Is(e, detect.ErrLimit) {
								code = "E_LIMIT"
								msg = "finding or candidate limit exceeded"
							}
							if ctx.Err() != nil {
								code = "E_CANCELLED"
								msg = "scan cancelled"
							}
							set(code, msg)
						} else if strings.EqualFold(filepath.Ext(it.path), ".csv") && !detect.CSVSafe(data, hits) {
							set("E_CSV", "rule match crosses CSV structural characters")
						} else {
							res.file = model.File{Path: it.path, Data: data, Digest: model.Hash(data), Info: st, Findings: hits}
						}
					}
				}
				results[i] = res
				done <- i
			}
		}()
	}
	go func() {
		defer close(work)
		for i := range items {
			work <- i
		}
	}()
	go func() { wg.Wait(); close(done) }()
	completed, findings := 0, 0
	for i := range done {
		completed++
		findings += len(results[i].file.Findings)
		if findings > p.Config.Limits.MaxFindings {
			v := model.Error("E_LIMIT", items[i].path, "total finding limit exceeded")
			results[i] = result{issue: &v}
		}
		if progress != nil {
			progress(Progress{completed, len(items), findings})
		}
	}
	for _, r := range results {
		if r.issue != nil {
			s.Issues = append(s.Issues, *r.issue)
		} else {
			s.Files = append(s.Files, r.file)
		}
	}
	if findings > p.Config.Limits.MaxFindings {
		fail("E_LIMIT", "", "total finding limit exceeded")
	}
	if ctx.Err() != nil && len(s.Issues) == 0 {
		fail("E_CANCELLED", "", "scan cancelled")
	}
	s.Complete = len(s.Issues) == 0
	return s
}

var errWalkLimit = errors.New("directory enumeration limit")

// Read bounded chunks rather than materializing an unbounded directory list.
// Results are sorted after enumeration; filesystem read order is not exposed.
func boundedWalk(ctx context.Context, root *os.Root, start string, budget int, fn fs.WalkDirFunc) error {
	count := 0
	var visit func(string, fs.DirEntry, int) error
	visit = func(name string, entry fs.DirEntry, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > budget || depth > 128 {
			return errWalkLimit
		}
		err := fn(name, entry, nil)
		if err == fs.SkipDir && entry.IsDir() {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		dir, err := root.Open(name)
		if err != nil {
			return fn(name, entry, err)
		}
		defer dir.Close()
		for {
			children, e := dir.ReadDir(128)
			for _, child := range children {
				if err := visit(filepath.ToSlash(filepath.Join(name, child.Name())), child, depth+1); err != nil {
					return err
				}
			}
			if e == io.EOF {
				return nil
			}
			if e != nil {
				return fn(name, entry, e)
			}
		}
	}
	st, err := root.Lstat(start)
	if err != nil {
		return fn(start, nil, err)
	}
	return visit(start, fs.FileInfoToDirEntry(st), 0)
}
