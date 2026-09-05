# Contributing to azform

Thanks for wanting to help. This project stays small on purpose, so the
bar is "does it earn its keep in the binary." That said, most useful
contributions are trivial to review and welcome.

## Ways to help (in order of impact)

1. **Bug reports with the command you were building.** The Azure CLI's
   help output is not perfectly uniform — the parser will hit cases I
   never saw. A copy of the `az … --help` output that broke it is worth
   more than a fix attempt without one.
2. **Missing-feature reports**, especially "I gave up and typed the
   command by hand because ___." That's the most useful signal there is.
3. **Small, targeted PRs.** Fixes to a specific parser edge case, a
   rendering bug, a keybinding annoyance.

Please open an issue *before* starting a large change — I might already
be planning it or actively working against it.

## Development setup

Requires **Go 1.26+** and, for lint, `golangci-lint`
(`brew install golangci-lint` or the equivalent).

```sh
git clone https://github.com/someson/azform.git
cd azform
make build       # ./bin/azform
make install     # $HOME/.local/bin/azform
make test        # unit tests
make test-race   # tests with the race detector
make lint        # must return "0 issues" before a PR is ready
make lint-fix    # auto-fix imports and other trivially fixable issues
```

## Coding conventions

- **Go idioms first.** If you're weighing "clever" vs "obvious", pick
  obvious.
- **Comments only when the *why* isn't obvious.** No comments that just
  restate the code. No block comments describing what the next 20 lines
  do — split into named helpers instead.
- **No new dependencies without discussion.** The binary should stay
  small and its supply chain shallow.
- **Tests for behavior, not implementation.** Prefer end-to-end tests
  through the public API of a package over asserting on internal state.
- **Errors carry context.** `fmt.Errorf("…: %w", err)` — wrap, don't
  swallow.

## Pull request checklist

Before you ask for review:

- [ ] `make test` passes
- [ ] `make lint` returns `0 issues`
- [ ] Commit messages describe the *why*, not the *what*. One
      concise line is fine; longer body if it needs one.
- [ ] The PR description says which issue it closes (if any) and
      what a reviewer should look at first.

CI runs the same `make test` + `make lint` on every push and PR. Green
CI is a hard requirement for merge.

## Commit messages

No enforced convention, but `type(scope): summary` reads well and
matches the existing history:

```
fix(ui): cap enum popup at 7 rows with scroll indicator
ci: bump golangci-lint-action v8→v9 to run natively on Node 24
docs(readme): note shell widget uses /dev/tty
```

Squash-merges collapse commits, so per-PR history doesn't need to be
pristine — just readable.

## Reporting security issues

See [SECURITY.md](SECURITY.md). Please don't file security issues in the
public tracker.

## Code of conduct

Be kind. Assume good faith. If someone's contribution needs work,
review the code, not the person. That's the whole rule.
