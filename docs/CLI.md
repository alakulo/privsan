# CLI & policy reference

Privsan reads one file or directory root per invocation. Directories are scanned recursively. Put flags **before** the root path. With no subcommand, Privsan runs scan; with no root, it scans the current directory.

## Commands

~~~text
privsan scan [options] [root]
privsan redact --output NEW_DIR [options] [root]
privsan redact --in-place --backup-dir DIR [options] [root]
privsan redact --stdout [--stdin | file] [--config FILE]
privsan tui [--output NEW_DIR | --in-place --backup-dir DIR] [options] [root]
privsan restore --from BACKUP_RUN --root ORIGINAL_ROOT [--json]
privsan config init [--output FILE]
privsan config validate --config FILE
privsan rules [--config FILE] [--json]
privsan version [--json]
~~~

scan never writes to source files. --dry-run disables writes in scan, redact and tui. redact --stdout accepts a single file or standard input and emits sanitized bytes. --stdin is also available for scan reports; it cannot be combined with filesystem writes.

For interactive review, see the [TUI guide](TUI.md), including pane navigation,
combined filters, file selection, sanitized previews and write confirmation.

## Scan options

| Option | Purpose |
|---|---|
| --config FILE | Load an explicit JSON policy |
| --json | Emit a JSON report |
| --jsonl | Emit file records followed by a summary record |
| --content | Include sanitized text; requires JSON or JSONL and complete success |
| --hide-paths | Omit paths from reports |
| --fail-on-findings | Return exit code 3 after a successful scan with findings |
| --dry-run | Disable all writes |
| --stdin | Read bounded UTF-8 standard input |
| --include GLOB | Include matching relative paths; repeatable |
| --exclude GLOB | Exclude matching relative paths; repeatable |
| --no-ignore | Do not read .privsanignore |
| --max-file BYTES | Override the per-file byte budget |
| --max-total BYTES | Override the batch byte budget |
| --max-files N | Override the file-count budget |
| --max-findings N | Override the finding-count budget |
| --workers N | Set concurrent readers, from 1 to 32 |

Defaults: 16 MiB per file, 128 MiB per batch, 10,000 files, 100,000 findings and min(CPU count, 4) workers. Bounds are validated rather than silently clamped. Exceeding a budget makes the scan incomplete and prevents writes and content output.

Directory traversal also has a max_files × 10 + 1,024 entry budget and a depth limit of 128. Candidate matches per file are limited to min(max_findings × 4, 2,000,000). The byte budget measures input, not total process memory.

## Filtering

Patterns match case-sensitive, root-relative paths using / as the separator:

- * matches within one path segment.
- ** matches zero or more directory segments.
- ? matches one character.
- A trailing / excludes the matching directory tree.

The root .privsanignore accepts one exclusion per line. Empty lines and lines starting with # are ignored. Negation with ! and nested ignore files are not supported. This is a separate syntax, not a .gitignore implementation.

By default, Privsan skips .git, node_modules, .privsan, symbolic links and special files. Hidden text files are otherwise eligible. It supports .md, .txt, .csv and .log, case-insensitively, and UTF-8 with or without a BOM. Invalid UTF-8 and NUL bytes are errors.

## Policy v1

Generate defaults with config init and validate edited policies with config validate. Policies are never discovered implicitly.

~~~json
{
  "version": 1,
  "disable": [],
  "id_validation": "candidate",
  "strategy": "placeholder",
  "hmac_key_env": "PRIVSAN_HMAC_KEY",
  "limits": {
    "max_file_bytes": 16777216,
    "max_total_bytes": 134217728,
    "max_files": 10000,
    "max_findings": 100000,
    "workers": 4
  },
  "include": [],
  "exclude": [],
  "rules": [
    {
      "id": "api_token",
      "pattern": "sk-[A-Za-z0-9]{12,}",
      "replacement": "[TOKEN]",
      "priority": 200
    }
  ]
}
~~~

See the [complete example](../privsan.example.json) and [policy schema](policy.schema.json). Runtime validation additionally checks regex syntax, duplicate keys, byte limits, path patterns and secrets.

### Built-in rules

| ID | Detection | Default replacement |
|---|---|---|
| phone | Mainland China mobile numbers, optional +86/86 and common 3–4–4 separators | [PHONE] |
| email | Common ASCII email addresses | [EMAIL] |
| id | 18-character Chinese ID candidates | [ID] |
| ipv4 | Validated IPv4 addresses | [IP] |
| ipv6 | Validated IPv6 addresses, including zone suffixes | [IP] |

id_validation may be candidate (default) or strict. Strict mode checks date and checksum; it does not establish authenticity or validate against a complete administrative-region database.

Email detection treats = as a log-field separator. Quoted addresses, Unicode addresses and unusual local parts containing = are outside the supported scope.

### Replacement strategies

| Strategy | Behavior |
|---|---|
| placeholder | Replace built-in matches with their type labels |
| mask | Replace built-in matches with a fixed [REDACTED] label |
| hmac | Emit stable [rule:hex] pseudonyms using HMAC-SHA256 |

HMAC uses the first 128 bits of the digest and separates values by rule type. Set the environment variable named by hmac_key_env to a secret of at least 32 bytes. Privsan does not store that secret in its reports. HMAC pseudonyms still expose equality; they are not anonymization.

Custom rules use Go RE2 syntax. IDs must match ^[a-z][a-z0-9_]{0,31}$. Up to 64 custom rules are allowed, with patterns up to 4,096 bytes, replacements up to 128 bytes and priorities between -1,000 and 1,000.

Replacements are literals: $1 is not expanded. Custom replacements take precedence over the global replacement strategy. Structural CSV and terminal-control characters are rejected. Zero-width matches fail the affected file.

Overlapping candidates are merged to cover the whole overlapping region. The representative rule is selected by earliest start, longest span, highest priority and then rule ID. Merging can combine findings that would otherwise be individually selectable.

Unknown fields, duplicate JSON keys, null values, unsupported policy versions and unknown disabled rule IDs are rejected.

## JSON report v2

The [report schema](report.schema.json) defines the structure. Reports contain:

| Field | Description |
|---|---|
| schema_version | 2 |
| version | Application version |
| mode | Scan or operation mode |
| complete | Whether the entire requested scan and operation succeeded |
| policy_id | Policy digest; does not include the HMAC secret |
| summary | Scanned files, skipped counts, bytes, findings and selected count |
| files | Per-file size and findings; optionally sanitized content |
| issues | Stable diagnostics without matched plaintext |
| operation | Optional operation status, changed-file count and run directory |

Finding IDs are local to each file. start_byte and end_byte are offsets into the original UTF-8 input; end_byte is exclusive. line and column are 1-based; columns count Unicode code points, not terminal display cells.

Paths are root-relative, or the basename for a single file and <stdin> for standard input. --hide-paths also removes issue paths and operation.run_dir. Reports do not expose matched plaintext or original-content hashes.

content is omitted by default. With --content, all file bodies are withheld unless complete=true. Consumers must also check the process exit code before using the output. Paths, unrecognized content and user-defined replacements can still contain sensitive information.

### JSONL

File records use {"type":"file","file":...}. The final record uses {"type":"summary","report":...}, with an empty report.files array. JSONL is sequential encoding, not a promise of streaming detection. Content is emitted only after the complete scan succeeds.

A missing final summary or a nonzero exit status must be treated as failure.

## Writes and recovery

--output requires a new directory outside the input root. Successful exports contain .privsan-export.json. An interrupted export may leave a partial directory without that marker.

--in-place requires --backup-dir outside the input root. Privsan verifies every source, persists and verifies backups, then commits files individually. The operation reports the backup run directory. Batches are not atomic; stop modifying sources while an operation runs.

restore requires both the backup run and the explicit original root. It validates every backup and destination before the first restoration. Files already restored are skipped. Files edited after redaction are rejected with E_CONFLICT.

Backups contain plaintext and are not encrypted. Protect the backup parent directory. For single-file input, the original root is its parent directory.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Complete success |
| 1 | Runtime failure or incomplete scan/operation |
| 2 | Invalid arguments or policy |
| 3 | Successful scan with findings, when --fail-on-findings is enabled |
| 130 | Cancellation |

Errors take precedence over findings. Ordinary successful scans return 0 even when they find sensitive data.

Diagnostics include E_CONFIG, E_INPUT, E_READ, E_CHANGED, E_ENCODING, E_LIMIT, E_RULE, E_CSV, E_WRITE, E_BACKUP, E_RESTORE, E_CONFLICT and E_CANCELLED. Scope skips are counted as S_EXTENSION, S_EXCLUDED, S_SYMLINK and S_SPECIAL.
