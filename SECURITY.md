# Security Policy

## Supported versions

Only the latest released version is supported. `azform` is pre-1.0;
older tags will not receive backports.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security reports.

Report privately via GitHub Security Advisories:
https://github.com/someson/azform/security/advisories/new

You'll get an acknowledgement within 3 working days and a fix or
mitigation plan within 14 days for confirmed issues.

## Scope

In scope:
- Command injection via parsed help text or field values that reach the
  assembled `az` command line.
- Path traversal or unsafe writes under `~/.local/state/azform/`.
- Unsafe handling of shell variable expansions (`$VAR`, `$(cmd)`, backticks).
- Terminal-escape injection in help text or Azure API responses that
  could corrupt the user's shell.

Out of scope:
- Vulnerabilities in the Azure CLI itself — report those upstream at
  https://github.com/Azure/azure-cli.
- `az login` credential handling. `azform` never touches Azure credentials
  directly; it only shells out to an already-authenticated `az` binary.
- Issues that require a compromised local shell or a malicious replacement
  of the `az` binary on `PATH` — the trust boundary starts at your shell.

## Disclosure

Once a fix is released we'll credit reporters (unless you'd rather stay
anonymous) in the release notes and, if impact warrants, publish a GitHub
Security Advisory.
