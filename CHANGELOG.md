# Changelog

## Unreleased

- Redesigned the TUI as a responsive workspace with file navigation, finding
  selection, scan counters and a scrollable fully sanitized preview.
- Added pane focus, combined live search and rule filters, file-scoped selection,
  light/dark palettes, progress feedback and keyboard help.
- Added scrollable write summaries with a default cancel action, persistent
  confirmation controls, and result/diagnostic views.
- Cached sanitized previews and isolated progress messages between scans.
- Kept dry-run, incomplete-scan, cancellation and backup guarantees intact.
- Updated the pinned govulncheck release to support the Go 1.27 AST.

## 1.0.0-rc.1

Initial public release candidate.

### Features

- Offline scanning of UTF-8 Markdown, text, CSV and log files.
- Detection of mainland China mobile numbers, common ASCII email addresses,
  18-character Chinese ID candidates, IPv4 and IPv6 addresses.
- Read-only previews and an asynchronous Bubble Tea interface with individual
  selection, filtering and masked line previews.
- Sanitized exports and in-place redaction with verified backups, conflict
  checks and repeatable restoration.
- JSON / JSONL reports, stdin / stdout, explicit content output and CI-friendly
  exit codes. Incomplete scans withhold all document content.
- Versioned policies, custom RE2 rules, fixed masks, HMAC pseudonyms and
  optional strict ID validation.
- Configurable scope, ignore patterns, concurrency and resource budgets.

### Compatibility

- JSON reports use schema version 2; policies use version 1.
- Use the scan, redact and tui subcommands. The earlier prototype's top-level
  --write / --tui flags and unversioned policies are not supported.
- See the [CLI reference](docs/CLI.md) for the current contract.

### Status

This is a release candidate. Build targets include Windows amd64, Linux
amd64/arm64 and macOS amd64/arm64; cross-compilation alone does not establish
native-platform support. A three-platform verification workflow is included.
