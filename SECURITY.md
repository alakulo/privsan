# Security

Privsan detects and redacts supported patterns in local text files. It is an
assistive tool, not a complete security audit. It does not guarantee
anonymization, zero missed detections or regulatory compliance.

## Runtime behavior

The application does not upload documents, collect telemetry, fetch remote
rules or require online activation. Building from source may download the Go
toolchain and dependencies. Matched plaintext is excluded from reports and
terminal previews; unrecognized content and file paths may still be sensitive.

## Files and recovery

Use a trusted local filesystem and keep source directories stationary during
scans and writes. Root-confined file access and source fingerprint checks do
not defend against privileged local attackers or eliminate every concurrent
filesystem race. Network filesystems and power-loss durability are outside the
current guarantee. Each file is committed individually; batches are not atomic.

Backups contain original plaintext and are not encrypted. On Windows, backup
files inherit the parent directory's ACL; select an appropriately protected
parent. On POSIX, newly created backup directories and files use restrictive
permissions. Not all ownership, ACL, extended-attribute or other metadata is
preserved across platforms.

Recovery manifests use hashes to detect corruption and state changes; they
are not signed tamper-proof records. Restore refuses to overwrite destination
content that differs from both the original and expected sanitized version.

## Policies and secrets

Only use policies from trusted sources: a policy can disable detection or
change replacements. HMAC keys are supplied through an environment variable
and are not persisted in reports. Stable pseudonyms reveal equality and are
not equivalent to anonymization.

Do not publish real inputs, local policies containing sensitive values,
environment secrets, output reports or backup runs. Use synthetic fixtures for
issues and tests.

## Reporting a vulnerability

Do not include real personal data or credentials in public issues. Use GitHub
private vulnerability reporting when it is enabled for this repository. If a
private reporting channel is not available, contact the maintainer privately
before disclosing sensitive details.

Include the version, platform, affected behavior and a minimal synthetic
reproduction. Ordinary bugs that do not expose sensitive data can be reported
as public issues with sanitized examples.
