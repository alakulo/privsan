# Contributing to Privsan

Contributions to detection rules, portability, documentation and tests are welcome.

## Set up

Use Go 1.26.5 or newer. go.mod selects Go 1.27.1 as the build toolchain; the first build may download it and the dependencies.

~~~sh
go build -trimpath -o privsan .
go test ./...
go vet ./...
~~~

For concurrency changes, run go test -race ./... with a supported C compiler. Python and jsonschema are needed only for the development-time schema checks in scripts/check-schemas.py.

## Changes

- Keep CLI examples and the [public reference](docs/CLI.md) consistent with behavior.
- Treat schema_version, policy versions and exit codes as public contracts.
- Add positive cases, negative cases and boundary tests when changing detection.
- Exercise incomplete scans and recovery failures when changing file operations.
- Keep matched plaintext out of logs, reports, errors and terminal previews.
- Use generated or synthetic fixtures. Never attach real personal data, credentials or backup files.

Business logic belongs in the shared workflow, detection or storage layers so that CLI and TUI enforce the same behavior. File changes must preserve the explicit-write, backup and conflict-detection guarantees.

## Report a bug

Include the application version, platform, command, exit code and a minimal synthetic reproduction. Remove sensitive values and secrets from any configuration you share. See [SECURITY.md](SECURITY.md) for security-related reports.

## Local files

Keep private policies, inputs, output reports and backups under ignored local directories such as private/, reports/ and backups/. Do not remove ignore rules merely to upload a reproduction; turn it into a small synthetic test fixture instead.

Product plans, architecture notes and local acceptance records are intentionally excluded from the public repository. Public usage docs, schemas, source, tests and verification scripts remain versioned.
