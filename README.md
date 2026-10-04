<div align="center">

<h1 align="center"><img src="images/privsan-logo.png" alt="Privsan" width="52" align="center" />&nbsp;Privsan</h1>

### Keep sensitive data local. Share only what you choose.

An offline CLI and terminal UI for sensitive-data redaction and custom text replacement in local files.

[![License](https://img.shields.io/badge/license-MIT-2563EB?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26.5%2B-00ADD8?style=flat-square&logo=go&logoColor=white)](go.mod)
[![Offline](https://img.shields.io/badge/runtime-offline-2E7D32?style=flat-square)](#privacy-model)
[![Formats](https://img.shields.io/badge/files-md%20%7C%20txt%20%7C%20csv%20%7C%20log-555?style=flat-square)](#supported-data)
[![Status](https://img.shields.io/badge/status-release%20candidate-D97706?style=flat-square)](#project-status)

[Quick start](#quick-start) · [Workflows](#workflows) · [AI agents](#ai-agent-integration) · [Reference](docs/CLI.md) · [简体中文](README.zh-CN.md)

</div>

---

Privsan helps you prepare logs, documents and test data before they leave your machine. Preview detections, review them in a [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI, then export sanitized copies or make backed-up changes in place.

**No uploads. No telemetry. No account required. Scanning is read-only by default.**

```text
contact=alice@example.com  phone=13800138000  source=192.0.2.1
                              ↓
contact=[EMAIL]           phone=[PHONE]     source=[IP]
```

> [!IMPORTANT]
> Detection is an aid, not a complete security audit. Regex rules can produce false positives and miss sensitive data. Review results before sharing them.

## Why Privsan?

- **Local by design.** File contents stay on your machine throughout scanning and redaction.
- **Preview first.** See match types and locations without touching source files.
- **Review interactively.** Select findings, filter by file or rule, and inspect redacted line previews.
- **Choose your workflow.** Export a new directory or edit in place with verified backups and recovery.
- **Integrate with agents.** JSON, JSONL and stdin/stdout fit local automation. Incomplete scans withhold content.
- **Replace your own text.** Literal find/replace, optional regex and case-insensitive search, with the same preview and backup workflow.
- **Bring your rules.** Add RE2 patterns, disable built-ins, or use fixed masks and stable HMAC pseudonyms.

## Quick start

Build from a checkout of this repository. Requires **Go 1.26.5+**; `go.mod` selects Go 1.27.1 as the build toolchain.

```sh
go build -trimpath -o privsan .

# Inspect the included synthetic sample without modifying it.
./privsan scan ./examples

# Open the review interface in read-only mode.
./privsan tui --dry-run ./examples
```

<details>
<summary><strong>Windows / PowerShell</strong></summary>

```powershell
go build -trimpath -o privsan.exe .
.\privsan.exe scan .\examples
.\privsan.exe tui --dry-run .\examples
```

</details>

The first source build may download Go dependencies and the toolchain. The resulting executable runs offline and does not require Go to be installed on the target machine.

Run `./privsan help` for all commands. Put options before the input path; each invocation accepts one file or directory root.

## Workflows

### 1. Scan without changing files

```sh
./privsan scan ./documents
./privsan scan --include '**/*.log' --exclude '**/generated/**' ./documents
```

Directories are scanned recursively. No command means `scan`; no path means the current directory. Use `.privsanignore` for persistent exclusions. Patterns are root-relative and are separate from `.gitignore`.

### 2. Review in the terminal

```sh
./privsan tui ./documents
```

![Privsan TUI walkthrough](images/tui.gif)

| Key | Action |
|:---|:---|
| `Tab` / `Shift+Tab` | Switch between files, findings and preview |
| `↑` / `↓`, `j` / `k`, `PgUp` / `PgDn` | Navigate the focused pane |
| `Space` | Toggle a finding or the current file's filtered findings |
| `/`, `[` / `]`, `Esc` | Live search, cycle rules, clear filters |
| `a` | Select or deselect all findings in the current scope |
| `←` / `→`, `h` / `l` | Scroll the focused preview horizontally |
| `o` / `d` | Set the input path / export directory |
| `f` / `Enter` | Focus files / review the selected file's findings |
| `c` / `Ctrl+D` | Custom find/replace form / return to privacy redaction |
| `w` | Review and confirm the write operation |
| `e` / `?` / `q` | View results and diagnostics / help / quit |

The workspace shows file navigation, finding selection, a scrollable sanitized preview and scan statistics. It adapts to smaller terminals and light or dark backgrounds. Previews mask **all detected values**, including deselected findings. Selection controls what is actually replaced.

Press `w` to review the write summary. Confirmation defaults to **Cancel**;
`Tab` switches the action, `Enter` activates it, and `y` executes explicitly.
If the export path is wrong, press **`d` inside the confirmation window** to
edit it with arrows, `Home` / `End` and `Backspace`. `Enter` updates the directory
and returns to the refreshed summary; `Esc` keeps the original directory.
Both return with Cancel selected. Dry-run mode never writes.

The minimum terminal size is 48 columns × 15 rows; 120 × 30 or larger is recommended. See the [TUI guide](docs/TUI.md) for scope selection, confirmation and cancellation.

### 3. Export sanitized copies

```sh
./privsan redact --output ./sanitized/batch-001 ./documents
```

The destination must be a **new directory outside the input root**. The relative directory structure is preserved for supported, included files. Source files remain untouched.

A successful export contains `.privsan-export.json`. Treat a nonzero exit status or a missing completion marker as an incomplete export.

### 4. Redact in place and restore

```sh
./privsan redact --in-place --backup-dir ./backups ./documents

# Use the run directory returned by the write operation.
./privsan restore --from ./backups/run-... --root ./documents
```

In-place changes require a backup directory outside the input root. Privsan verifies all sources and backups before committing files individually. Restore refuses to overwrite files edited after redaction.

> [!NOTE]
> Backups contain the original plaintext. Keep them in a protected directory. Writes are committed per file, not as an atomic whole-directory transaction.

### 5. Replace your own text

```sh
./privsan replace --find 'old project' --with 'new project' --dry-run ./documents
./privsan replace --find 'old project' --with 'new project' --output ../replaced-copy ./documents

# Optional regex and case-insensitive search.
./privsan replace --find 'build-[0-9]+' --with 'build-current' --regex --ignore-case --dry-run ./documents

# Delete matching text with an explicit empty replacement.
./privsan replace --find 'temporary note' --with= --dry-run ./notes.txt
```

In the TUI, select a file with `f` and press `c` to replace within that file, or
open the form from the all-files scope for batch replacement. Fill in **find /
replace with**, then submit with `Enter`, select matches with `Space`, set a new
export directory with `d`, and review the write with `w`. `Esc` cancels the form.

| Form key | Action |
|:---|:---|
| `Tab` / `Shift+Tab` | Switch between find and replacement fields |
| `Ctrl+S` | Switch current-file / all-file scope when a file is available |
| `Ctrl+R` | Toggle literal / Go regular expression search |
| `Ctrl+G` | Toggle case-sensitive / Unicode case-insensitive search |
| `Enter` / `Esc` | Generate a preview / cancel the draft |

`Ctrl+D` on the main screen returns to privacy redaction. New plans reset
matches and selections. Search is literal and case-sensitive by default;
replacement text is always literal, including `$1` and `\n`. An empty replacement
deletes matches. The TUI accepts single-line input; use the CLI for actual
newlines or tabs. The default **16 MiB per file / 128 MiB per batch** budgets also
bound replacement expansion.

**Custom replacement does not automatically redact sensitive data.** Its TUI
preview shows all replacement candidates and masks identified privacy for review;
output applies only selected custom replacements. Preview privacy masks are not
written to files, and `replace --content` / `--stdout` may retain sensitive data.
Use `scan` / `redact` for agent privacy workflows.
[Replacement reference →](docs/CLI.md#custom-find-and-replace)

## AI agent integration

Run Privsan in a trusted local process **before** an agent reads the document:

```sh
./privsan scan --json --content --hide-paths ./documents
```

- `--json` returns metadata by default. Add `--content` to request sanitized text.
- Content is returned only when the entire scan and requested operation succeed.
- Check the process exit code and `complete: true`, then use only `files[].content`.
- `--hide-paths` omits paths from the report. It does not change the source filenames.

For a single text stream:

```sh
printf 'contact=alice@example.com\n' | ./privsan redact --stdin --stdout
# contact=[EMAIL]
```

Use `--jsonl` for file records followed by a final summary. Consumers must verify that final record and the process exit code. [Read the report contract →](docs/CLI.md#json-report-v2)

## Supported data

**Files:** UTF-8 `.md`, `.txt`, `.csv` and `.log`, including UTF-8 BOMs. Unmatched bytes and line endings are preserved.

| Detection | Scope | Default mask |
|:---|:---|:---|
| Phone numbers | Mainland China mobile numbers; optional country code and common separators | `[PHONE]` |
| Email addresses | Common ASCII email addresses | `[EMAIL]` |
| Chinese ID numbers | 18-character candidates; optional date and checksum validation | `[ID]` |
| IP addresses | Validated IPv4 and IPv6, including IPv6 zone suffixes | `[IP]` |

CSV is processed as text. Matches crossing structural delimiters, quotes or line endings are rejected. Custom replacements also cannot insert commas, semicolons, quotes or line endings into CSV. International phone numbers, Unicode/quoted email addresses, Office documents, PDFs and image recognition are outside the current scope.

## Configuration

Create, edit and validate an explicit policy:

```sh
./privsan config init --output policy.json
./privsan config validate --config policy.json
./privsan scan --config policy.json ./documents
```

Add a custom rule to the policy's `rules` array:

```json
{
  "id": "api_token",
  "pattern": "sk-[A-Za-z0-9]{12,}",
  "replacement": "[TOKEN]",
  "priority": 200
}
```

Replacements are literal strings; `$1` is not expanded. Built-in strategies include type placeholders, a fixed `[REDACTED]` mask, and HMAC pseudonyms whose key is supplied through an environment variable.

See the [example policy](privsan.example.json), [configuration reference](docs/CLI.md#policy-v1) and [policy schema](docs/policy.schema.json).

## Privacy model

Privsan's runtime does not upload documents, collect telemetry, fetch remote policies or require online activation. Metadata reports omit matched source snippets and custom search queries. Terminal content previews mask detected privacy; custom replacement output follows the separate contract above.

Unrecognized content, filenames and paths may still contain sensitive information. Redaction does not guarantee anonymization or compliance. Source directories should remain stationary while scanning or writing; backups are unencrypted. Read [SECURITY.md](SECURITY.md) for the full boundary.

Default budgets are **16 MiB per file**, **128 MiB per batch**, **10,000 files**, **100,000 findings** and up to **4 concurrent readers**. Exceeding a budget is an error, not silent truncation. [Limits and exit codes →](docs/CLI.md)

## Documentation & contributing

| Resource | What it covers |
|:---|:---|
| [CLI reference](docs/CLI.md) | Commands, policies, filtering, JSON and exit codes |
| [JSON schemas](docs/report.schema.json) | Machine-readable report structure |
| [Operations guide / 操作手册](docs/OPERATIONS.md) | Recovery and troubleshooting, in Chinese |
| [Contributing](CONTRIBUTING.md) | Setup, verification and contribution guidelines |
| [Changelog](CHANGELOG.md) | Release history |
| [Security](SECURITY.md) | Threat boundaries and reporting guidance |

For a local check:

```sh
go test ./...
go vet ./...
```

Use synthetic fixtures in bug reports and contributions. Never include real personal data, keys or plaintext backups.

## Project status

**1.0.0-rc.1 — release candidate.** Build targets include Windows amd64, Linux amd64/arm64 and macOS amd64/arm64. Cross-compilation is not the same as native-platform verification; the repository includes a three-platform CI workflow.

## License

Licensed under the [MIT License](LICENSE). Third-party notices are documented in [NOTICE.md](NOTICE.md).
