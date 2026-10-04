package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"
	"privsan/internal/detect"
	"privsan/internal/model"
	"privsan/internal/policy"
	"privsan/internal/replace"
	"privsan/internal/scan"
	"privsan/internal/storage"
	"privsan/internal/tui"
	"privsan/internal/workflow"
)

type stringsFlag []string

func (s *stringsFlag) String() string     { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(v string) error { *s = append(*s, v); return nil }

type streams struct {
	in       io.Reader
	out, err io.Writer
}

func Run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) int {
	st := streams{in, out, stderr}
	if len(args) > 0 && (args[0] == "--version" || args[0] == "version") {
		return version(args[1:], st)
	}
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		_, err := io.WriteString(out, help)
		if err != nil {
			return 1
		}
		return 0
	}
	cmd := "scan"
	if len(args) > 0 {
		switch args[0] {
		case "scan", "redact", "replace", "tui", "restore", "config", "rules":
			cmd = args[0]
			args = args[1:]
		}
	}
	switch cmd {
	case "config":
		return configCommand(args, st)
	case "rules":
		return rulesCommand(args, st)
	case "restore":
		return restoreCommand(ctx, args, st)
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	config := fs.String("config", "", "configuration path")
	structured := fs.Bool("json", false, "JSON report")
	jsonl := fs.Bool("jsonl", false, "JSON lines report")
	content := fs.Bool("content", false, "include sanitized content on complete success")
	hide := fs.Bool("hide-paths", false, "omit paths in report")
	failFindings := fs.Bool("fail-on-findings", false, "exit 3 when findings exist")
	dry := fs.Bool("dry-run", false, "never write")
	stdin := fs.Bool("stdin", false, "read UTF-8 standard input")
	stdout := fs.Bool("stdout", false, "write sanitized bytes to stdout")
	output := fs.String("output", "", "new output directory")
	inplace := fs.Bool("in-place", false, "replace input files")
	backup := fs.String("backup-dir", "", "backup parent directory")
	noignore := fs.Bool("no-ignore", false, "ignore .privsanignore")
	maxFile := fs.Int64("max-file", -1, "maximum file bytes")
	maxTotal := fs.Int64("max-total", -1, "maximum batch bytes")
	maxFiles := fs.Int("max-files", -1, "maximum file count")
	maxFindings := fs.Int("max-findings", -1, "maximum findings")
	workers := fs.Int("workers", -1, "scan workers")
	find := fs.String("find", "", "custom search text")
	with := fs.String("with", "", "literal replacement; empty deletes matches")
	regex := fs.Bool("regex", false, "use Go regular expression search")
	ignoreCase := fs.Bool("ignore-case", false, "Unicode case-insensitive search")
	var includes, excludes stringsFlag
	fs.Var(&includes, "include", "include glob")
	fs.Var(&excludes, "exclude", "exclude glob")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			_, err = io.WriteString(out, help)
			if err != nil {
				return 1
			}
			return 0
		}
		return usageError(st, "invalid arguments; run privsan help")
	}
	if len(fs.Args()) > 1 {
		return usageError(st, "supply one file or directory root; put flags before paths")
	}
	findSet, withSet, searchFlags := false, false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "find":
			findSet, searchFlags = true, true
		case "with":
			withSet, searchFlags = true, true
		case "regex", "ignore-case":
			searchFlags = true
		}
	})
	if searchFlags && cmd != "replace" && cmd != "tui" ||
		(cmd == "replace" || searchFlags) && (!findSet || !withSet) {
		return usageError(st, "replace requires --find and --with; search options are supported only by replace and tui")
	}
	var replacement *replace.Plan
	if findSet {
		var e error
		replacement, e = replace.Compile(replace.Options{Find: *find, With: *with, Regex: *regex, IgnoreCase: *ignoreCase})
		if e != nil {
			return usageError(st, e.Error())
		}
	}
	if *structured && *jsonl || *content && !(*structured || *jsonl) || *stdin && len(fs.Args()) > 0 {
		return usageError(st, "conflicting output or input options")
	}
	writeOpts := workflow.WriteOptions{Output: *output, BackupDir: *backup, InPlace: *inplace, DryRun: *dry}
	hasWrite := *output != "" || *inplace || *backup != ""
	if *inplace && (*output != "" || *backup == "") || *backup != "" && !*inplace {
		return usageError(st, "in-place requires backup-dir and excludes output")
	}
	if cmd == "scan" && (hasWrite || *stdout) {
		return usageError(st, "scan does not accept write targets")
	}
	if cmd == "tui" && (*structured || *jsonl || *stdin || *stdout || *content) {
		return usageError(st, "TUI requires a terminal and does not support pipe/report flags")
	}
	if *stdout && (cmd != "redact" && cmd != "replace" || hasWrite || *structured || *jsonl || *content || *dry) {
		return usageError(st, "stdout mode excludes file writes, dry-run and reports")
	}
	if (cmd == "redact" || cmd == "replace") && !*dry && !hasWrite && !*stdout {
		return usageError(st, "redact/replace requires dry-run, output, in-place with backup-dir, or stdout")
	}
	if *stdin && (hasWrite || cmd == "tui") {
		return usageError(st, "stdin cannot be used with filesystem writes or TUI")
	}
	c, err := policy.Load(*config)
	if err != nil {
		return usageError(st, "configuration could not be loaded or is invalid")
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "max-file":
			c.Limits.MaxFile = *maxFile
		case "max-total":
			c.Limits.MaxTotal = *maxTotal
		case "max-files":
			c.Limits.MaxFiles = *maxFiles
		case "max-findings":
			c.Limits.MaxFindings = *maxFindings
		case "workers":
			c.Limits.Workers = *workers
		}
	})
	p, err := policy.Compile(c)
	if err != nil {
		return usageError(st, err.Error())
	}
	root := "."
	if len(fs.Args()) == 1 {
		root = fs.Args()[0]
	}
	opts := scan.Options{Root: root, Include: includes, Exclude: excludes, NoIgnore: *noignore, Replacement: replacement}
	for _, g := range append(append(append(append([]string{}, c.Include...), c.Exclude...), includes...), excludes...) {
		if _, err = scan.CompileGlob(g); err != nil {
			return usageError(st, "invalid include/exclude glob")
		}
	}
	svc := workflow.Service{Policy: p}
	var snapshot model.Snapshot
	var op model.Operation
	if cmd == "tui" {
		input, ok := in.(*os.File)
		if !ok || !term.IsTerminal(input.Fd()) {
			return usageError(st, "TUI requires an interactive terminal")
		}
		outputFile, ok := out.(*os.File)
		if !ok || !term.IsTerminal(outputFile.Fd()) {
			return usageError(st, "TUI output must be a terminal")
		}
		snapshot, op, err = tui.Run(ctx, svc, opts, writeOpts, input, outputFile)
		if err != nil {
			if ctx.Err() != nil {
				return 130
			}
			fmt.Fprintln(stderr, "E_INPUT: terminal session failed")
			return 1
		}
	} else if *stdin {
		snapshot = readStdin(ctx, in, p, replacement)
	} else {
		if *stdout {
			info, e := os.Stat(root)
			if e != nil || !info.Mode().IsRegular() {
				return usageError(st, "stdout mode requires one regular file or stdin")
			}
		}
		snapshot = svc.Scan(ctx, opts, nil)
	}
	if (cmd == "redact" || cmd == "replace") && !*stdout {
		op = svc.Write(ctx, &snapshot, writeOpts)
	}
	mode := cmd
	if cmd == "tui" && snapshot.PolicyID == "replace-v1" {
		mode = "replace"
	}
	if *dry {
		mode = "dry-run"
	}
	if op.Kind != "" {
		mode = op.Kind
	}
	report := model.BuildReport(snapshot, op, model.ReportOptions{Mode: mode, Content: *content, HidePaths: *hide})
	if *stdout {
		if !report.Complete {
			printIssues(stderr, report)
			return reportExit(ctx, report, false)
		}
		if len(snapshot.Files) != 1 {
			fmt.Fprintln(stderr, "E_INPUT: expected exactly one file")
			return 1
		}
		b, e := model.Render(snapshot.Files[0].Data, snapshot.Files[0].Findings, false)
		if e != nil {
			fmt.Fprintln(stderr, "E_RULE: invalid findings")
			return 1
		}
		if _, e = out.Write(b); e != nil {
			return 1
		}
	} else if err = WriteReport(out, report, *structured, *jsonl); err != nil {
		return 1
	}
	if !*structured && !*jsonl {
		printIssues(stderr, report)
	}
	return reportExit(ctx, report, *failFindings)
}
func readStdin(ctx context.Context, in io.Reader, p *policy.Policy, replacements ...*replace.Plan) model.Snapshot {
	s := model.Snapshot{PolicyID: p.ID, Files: []model.File{}, Issues: []model.Issue{}, Skipped: map[string]int{}}
	// A stalled producer must not prevent Ctrl+C from returning. The buffered
	// channel lets an eventual read completion release its data after cancel.
	type inputResult struct {
		data []byte
		err  error
	}
	inputDone := make(chan inputResult, 1)
	go func() {
		b, e := io.ReadAll(io.LimitReader(in, p.Config.Limits.MaxFile+1))
		inputDone <- inputResult{b, e}
	}()
	var data []byte
	var err error
	select {
	case result := <-inputDone:
		data, err = result.data, result.err
	case <-ctx.Done():
		err = ctx.Err()
	}
	code, msg := "", ""
	if err != nil {
		code, msg = "E_READ", "cannot read stdin"
	} else if int64(len(data)) > p.Config.Limits.MaxFile {
		code, msg = "E_LIMIT", "stdin size limit exceeded"
	} else if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		code, msg = "E_ENCODING", "stdin must contain UTF-8 text without NUL"
	}
	if ctx.Err() != nil {
		code, msg = "E_CANCELLED", "stdin scan cancelled"
	}
	if code != "" {
		s.Issues = append(s.Issues, model.Error(code, "<stdin>", msg))
		return s
	}
	var hits []model.Finding
	if len(replacements) > 0 && replacements[0] != nil {
		s.PolicyID = "replace-v1"
		hits, err = replacements[0].Find(ctx, data, p.Config.Limits.MaxFindings, p.Config.Limits.MaxFile)
	} else {
		hits, err = detect.Find(ctx, data, p, p.Config.Limits.MaxFindings)
	}
	if err != nil {
		code = "E_RULE"
		if err == detect.ErrLimit || err == replace.ErrLimit {
			code = "E_LIMIT"
		}
		if ctx.Err() != nil {
			code = "E_CANCELLED"
		}
		s.Issues = append(s.Issues, model.Error(code, "<stdin>", "stdin rule evaluation failed"))
		return s
	}
	s.Files = append(s.Files, model.File{Path: "<stdin>", Data: data, Digest: model.Hash(data), Findings: hits})
	s.Complete = true
	return s
}
func usageError(st streams, msg string) int { fmt.Fprintf(st.err, "E_CONFIG: %s\n", msg); return 2 }
func reportExit(ctx context.Context, r model.Report, fail bool) int {
	if ctx.Err() != nil {
		return 130
	}
	for _, i := range r.Issues {
		if i.Code == "E_CANCELLED" {
			return 130
		}
	}
	if r.Operation != nil {
		for _, i := range r.Operation.Issues {
			if i.Code == "E_CANCELLED" {
				return 130
			}
		}
	}
	if !r.Complete {
		return 1
	}
	if fail && r.Summary.Findings > 0 {
		return 3
	}
	return 0
}
func printIssues(w io.Writer, r model.Report) {
	issues := append([]model.Issue{}, r.Issues...)
	if r.Operation != nil {
		issues = append(issues, r.Operation.Issues...)
	}
	for _, i := range issues {
		fmt.Fprintf(w, "%s %q: %s\n", i.Code, i.Path, i.Message)
	}
}
func WriteReport(w io.Writer, r model.Report, jsonMode, jsonl bool) error {
	enc := json.NewEncoder(w)
	if jsonMode {
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	if jsonl {
		for _, f := range r.Files {
			if err := enc.Encode(struct {
				Type string           `json:"type"`
				File model.ReportFile `json:"file"`
			}{"file", f}); err != nil {
				return err
			}
		}
		r.Files = []model.ReportFile{}
		return enc.Encode(struct {
			Type   string       `json:"type"`
			Report model.Report `json:"report"`
		}{"summary", r})
	}
	for _, f := range r.Files {
		for _, h := range f.Findings {
			if _, err := fmt.Fprintf(w, "%q:%d:%d  %-8s -> %q  selected=%t\n", f.Path, h.Line, h.Column, h.Rule, h.Replacement, h.Selected); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(w, "%s: complete=%t files=%d findings=%d selected=%d bytes=%d\n", r.Mode, r.Complete, r.Summary.Files, r.Summary.Findings, r.Summary.Selected, r.Summary.Bytes); err != nil {
		return err
	}
	if r.Operation != nil {
		_, err := fmt.Fprintf(w, "operation: complete=%t changed=%d run_dir=%q\n", r.Operation.Complete, r.Operation.Changed, r.Operation.RunDir)
		return err
	}
	return nil
}
func version(args []string, st streams) int {
	if len(args) > 1 || len(args) == 1 && args[0] != "--json" {
		return usageError(st, "version supports only --json")
	}
	v := struct{ Version, Commit, BuildDate string }{model.Version, model.Commit, model.BuildDate}
	var err error
	if len(args) == 1 {
		err = json.NewEncoder(st.out).Encode(v)
	} else {
		_, err = fmt.Fprintf(st.out, "privsan %s (%s, %s)\n", v.Version, v.Commit, v.BuildDate)
	}
	if err != nil {
		return 1
	}
	return 0
}
func configCommand(args []string, st streams) int {
	if len(args) == 0 {
		return usageError(st, "config requires init or validate")
	}
	cmd := args[0]
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := fs.String("output", "", "new file")
	path := fs.String("config", "", "policy file")
	if fs.Parse(args[1:]) != nil || len(fs.Args()) != 0 {
		return usageError(st, "invalid config arguments")
	}
	switch cmd {
	case "init":
		if *path != "" {
			return usageError(st, "init does not accept config")
		}
		b, _ := json.MarshalIndent(policy.Default(), "", "  ")
		b = append(b, '\n')
		if *output == "" {
			if _, e := st.out.Write(b); e != nil {
				return 1
			}
			return 0
		}
		f, e := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			fmt.Fprintln(st.err, "E_WRITE: config target must not exist")
			return 1
		}
		_, e = f.Write(b)
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil || ce != nil {
			fmt.Fprintln(st.err, "E_WRITE: cannot save configuration")
			return 1
		}
		return 0
	case "validate":
		if *path == "" || *output != "" {
			return usageError(st, "validate requires config and excludes output")
		}
		c, e := policy.Load(*path)
		if e != nil {
			return usageError(st, "invalid configuration")
		}
		p, e := policy.Compile(c)
		if e != nil {
			return usageError(st, e.Error())
		}
		for _, g := range append(append([]string{}, c.Include...), c.Exclude...) {
			if _, e = scan.CompileGlob(g); e != nil {
				return usageError(st, "invalid path pattern")
			}
		}
		if _, e = fmt.Fprintf(st.out, "valid policy %s (%d rules)\n", p.ID, len(p.Rules)); e != nil {
			return 1
		}
		return 0
	default:
		return usageError(st, "unknown config subcommand")
	}
}
func rulesCommand(args []string, st streams) int {
	fs := flag.NewFlagSet("rules", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	config := fs.String("config", "", "policy")
	structured := fs.Bool("json", false, "JSON")
	if fs.Parse(args) != nil || len(fs.Args()) != 0 {
		return usageError(st, "invalid rules arguments")
	}
	c, e := policy.Load(*config)
	if e != nil {
		return usageError(st, "invalid configuration")
	}
	p, e := policy.Compile(c)
	if e != nil {
		return usageError(st, e.Error())
	}
	if *structured {
		rr := []policy.Rule{}
		for _, r := range p.Rules {
			rr = append(rr, r.Rule)
		}
		if e = json.NewEncoder(st.out).Encode(rr); e != nil {
			return 1
		}
	} else {
		for _, r := range p.Rules {
			if _, e = fmt.Fprintf(st.out, "%s  priority=%d  builtin=%t\n", r.ID, r.Priority, r.Builtin); e != nil {
				return 1
			}
		}
	}
	return 0
}
func restoreCommand(ctx context.Context, args []string, st streams) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	from := fs.String("from", "", "backup run")
	root := fs.String("root", "", "allowed root")
	structured := fs.Bool("json", false, "JSON")
	if fs.Parse(args) != nil || len(fs.Args()) != 0 || *from == "" || *root == "" {
		return usageError(st, "restore requires from and root")
	}
	op := storage.Restore(ctx, *from, *root)
	report := model.BuildReport(model.Snapshot{Complete: true}, op, model.ReportOptions{Mode: "restore"})
	if WriteReport(st.out, report, *structured, false) != nil {
		return 1
	}
	if !*structured {
		printIssues(st.err, report)
	}
	return reportExit(ctx, report, false)
}

const help = `Privsan — offline file privacy

Usage:
  privsan scan [options] [root]
  privsan redact --output NEW_DIR [options] [root]
  privsan redact --in-place --backup-dir DIR [options] [root]
  privsan redact --stdout [--stdin | file]
  privsan replace --find TEXT --with TEXT --dry-run [options] [root]
  privsan replace --find TEXT --with TEXT --output NEW_DIR [options] [root]
  privsan replace --find TEXT --with TEXT --in-place --backup-dir DIR [root]
  privsan replace --find TEXT --with TEXT --stdout [--stdin | file]
  privsan tui [--output NEW_DIR | --in-place --backup-dir DIR] [root]
  privsan restore --from BACKUP_RUN --root ORIGINAL_ROOT [--json]
  privsan config init [--output FILE]
  privsan config validate --config FILE
  privsan rules [--config FILE] [--json]
  privsan version [--json]

Scan options (put flags before the root path):
  --config FILE         Explicit versioned policy
  --json / --jsonl       Structured metadata report
  --content             Include sanitized text only on complete success
  --hide-paths          Omit paths from structured reports
  --fail-on-findings    Exit 3 on a successful scan with findings
  --dry-run             Disable every write
  --stdin               Read bounded UTF-8 standard input
  --include GLOB        Include relative paths; repeatable
  --exclude GLOB        Exclude relative paths; repeatable
  --no-ignore           Do not read root .privsanignore
  --max-file BYTES       Per-file input budget (default 16 MiB)
  --max-total BYTES      Batch input budget (default 128 MiB)
  --max-files N          File count budget (default 10000)
  --max-findings N       Finding budget (default 100000)
  --workers N           Concurrent readers (1..32)

Custom replace (replace / tui):
  --find TEXT           Nonempty search, at most 4096 UTF-8 bytes
  --with TEXT           Literal replacement; explicit empty value deletes
  --regex               Go regular expression; zero-width matches rejected
  --ignore-case         Unicode case-insensitive matching
Replacement is separate from privacy redaction; $1 and backslashes are literal.
Output expansion is bounded by max-file and max-total, including partial selections.

Default command: scan. Default root: current directory.
In-place writes require backups; never modify a directory concurrently.
Exit: 0 success, 1 incomplete/error, 2 usage/config, 3 findings, 130 cancelled.
Detection is assistance, not a complete security audit.
`
