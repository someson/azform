# azform

**A form for building Azure CLI commands, right in your terminal.**

[![License](https://img.shields.io/github/license/someson/azform)](LICENSE)
[![CI](https://github.com/someson/azform/actions/workflows/ci.yml/badge.svg)](https://github.com/someson/azform/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/someson/azform)](https://github.com/someson/azform/releases/latest)
[![Downloads](https://img.shields.io/github/downloads/someson/azform/total)](https://github.com/someson/azform/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/someson/azform)](go.mod)
[![Issues](https://img.shields.io/github/issues/someson/azform)](https://github.com/someson/azform/issues)
[![Last commit](https://img.shields.io/github/last-commit/someson/azform)](https://github.com/someson/azform/commits/main)
[![PRs welcome](https://img.shields.io/badge/PRs-welcome-brightgreen)](CONTRIBUTING.md)
[![Stars](https://img.shields.io/github/stars/someson/azform?style=social)](https://github.com/someson/azform/stargazers)

> Status: early development.

---

```
az network public-ip create
Create a public IP address.
───────────────────────────────────────────────────────────────────────────────
● --name                 pip-nat               ○ --acquire-policy-token —
● --resource-group       myResourceGroup       ○ --change-reference     —
● --allocation-method    Static                ○ --debug
○ --ddos-protection-mode —                     ○ --help
○ --ddos-protection-plan —                     ○ --only-show-errors
○ --dns-name             —                     ● --output               tsv
○ --dns-name-scope       —                     ○ --query                —
○ --edge-zone            —                     ○ --subscription         —
● --idle-timeout         4                     ○ --verbose
○ --ip-address           —
○ --ip-tags              —
○ --location             —
○ --public-ip-prefix     —
○ --reverse-fqdn         —
● --sku                  StandardV2
○ --tags                 —
○ --tier                 —
● --version              IPv4
○ --zone                 —

───────────────────────────────────────────────────────────────────────────────
az network public-ip create \
  --name pip-nat \
  --resource-group myResourceGroup \
  --allocation-method Static \
  --idle-timeout 4 \
  --sku StandardV2 \
  --version IPv4 \
  --output tsv

 Done    Cancel                                               Press F1 for help
───────────────────────────────────────────────────────────────────────────────
```

## What it is

`azform` is a small terminal companion for the Azure CLI.

You press a key. A form opens under your prompt, listing every parameter the command accepts. Required ones are marked. Parameters with a fixed set of allowed values show that set, so you pick instead of typing. When you're done, the assembled command lands in your prompt — ready for you to read, adjust, and run.

It is a way to *write* `az` commands. Nothing more than that, and that's the point.

**Under the hood.** `azform` is a single Go binary. When you open a form, it shells out to `az <command> --help` and parses the text — the same help you'd read yourself. It attaches as a shell widget (Ctrl-X Ctrl-A in zsh), takes over the terminal via `/dev/tty` while the form is open, and writes the finished command straight into your shell's line buffer on exit. No daemon, no telemetry, no phone-home. Small state — drafts, remembered variable bindings — lives under `~/.local/state/azform/` as plain files you can delete at any time.

## What it is not

- **Not a replacement for the Azure CLI.** What comes out is a plain `az` command. Paste it into a script, a pipeline, a message to a colleague, or a bug report. It reads exactly like the documentation.
- **Not a runner.** `azform` never executes the command you're building. It hands it to you and steps aside — you press Enter yourself. It does ask the Azure CLI for help text, and, if you let it, reads lists of your existing resources. It never writes anything to Azure, and it never runs a command you didn't run yourself.
- **Not a wrapper with its own syntax.** No new commands to learn, no abstraction over Azure concepts, no leaky translation layer between you and the CLI you already know.
- **Not a background service.** It runs when you press the key and exits when you're done. Nothing sits in memory, nothing starts with your machine.
- **Deterministic.** No suggestions about what you *probably* meant, no generated commands, no network calls to anyone but Azure.

## Who it's for

People who use the Azure CLI regularly enough to be annoyed by it, but not often enough to have memorized it.

If you know exactly which parameters `az storage account create` takes and how each value is spelled, you don't need this. If you find yourself opening the documentation in a browser to check whether it's `TLS1_2` or `TLSv1.2`, or running the command three times to discover which parameters were required after all — that's the gap this fills.

It assumes you know Azure. It does not assume you know the CLI by heart.

## Why I built it

The Azure CLI is excellent at what it was designed for: scripting and automation. Commands are long, explicit, and unambiguous — exactly right for a file that runs unattended.

That same design is tiring to type by hand. A single command can take dozens of parameters. Some are required, some aren't, and the only way to find out is to run it and read the error. Values are case-sensitive and inconsistently formatted across services. Tab completion helps with the next token, but it can't show you the shape of the whole command, and it can't tell you what you're still missing.

So the loop becomes: type, run, read error, fix, run again. Sometimes four or five times for one resource.

`azform` replaces the guessing part of that loop with a form. Everything the command accepts is visible at once, what's required is marked as required, and closed value sets are lists you choose from. The typing is still yours. The remembering isn't.

## What it can do

- Show every parameter of a command in one place, with required ones marked
- Filter the parameter list as you type — names and help text are searched live, so a 100-parameter command like `az vm create` collapses to one row when you know what you're after
```
○ --dns-name               —
○ --dns-name-scope         —

/ dns█
```
- Turn fixed value sets into pickable lists, so misspellings stop happening
```
● --name                 pip-nat                   ○ --acquire-policy-token —
● --resource-group       myResourceGroup           ○ --change-reference     —
● --allocation-method    Static                    ○ --debug
○ --ddos-protection-mode —                         ○ --help
○ --ddos-protection-plan —                         ○ --only-show-errors
○ --dns-name             —                         ● --output               json
○ --dns-name-scope       —                         ┌──────────────────────┐ —
○ --edge-zone            —                         │▶ json                │ —
● --idle-timeout         4                         │  jsonc               │
○ --ip-address           —                         │  none                │
○ --ip-tags              —                         │  table               │
○ --location             —                         │  tsv                 │
○ --public-ip-prefix     —                         │  yaml                │
○ --reverse-fqdn         —                         │  yamlc               │
● --sku                  StandardV2                └──────────────────────┘
○ --tags                 —
○ --tier                 —
● --version              IPv4
○ --zone                 —
```
- Pick up a command you already started typing and let you finish it in the form
- Fill fields with shell variables you already have defined, and remember which variable you used for which parameter
- Warn you before you run a command that references a variable your shell doesn't actually have
- Open a filtered variable picker from any field (`Ctrl-G`) to insert `$VAR` from the current shell session without scrolling through your whole env
```
● --name                 pip-nat                   ○ --acquire-policy-token —
● --resource-group       █                         ○ --change-reference     —
┌─────────────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ filter: my█                                                                                                     │
│▶ myResourceGroup                                                                                                │
│  my_git_format                                                                                                  │
│                                                                                                                 │
│                                                                                                                 │
│                                                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
● --allocation-method    Static                    ○ --debug
○ --ddos-protection-mode —                         ○ --help
○ --ddos-protection-plan —                         ○ --only-show-errors
○ --dns-name             —                         ● --output               json
○ --dns-name-scope       —                         ○ --query                —
○ --edge-zone            —                         ○ --subscription         —
● --idle-timeout         4                         ○ --verbose
○ --ip-address           —
○ --ip-tags              —
○ --location             —
○ --public-ip-prefix     —
○ --reverse-fqdn         —
● --sku                  StandardV2
○ --tags                 —
○ --tier                 —
● --version              IPv4
○ --zone                 —
```
- Show live values from your Azure subscription where it makes sense — resource groups, locations, existing resources
- Save named presets, so "a storage account like the one in project X" is one keystroke
- Give the finished command back as a single line, as a multi-line script block, or on your clipboard

## What it can't do

- **Tell you what to build.** It shows you the parameters; deciding what belongs in them is your job.
- **Validate against Azure before you run.** Some things are only knowable by trying. It catches missing required parameters, wrong enum values, and undefined variables — not quota limits, naming conflicts, or permission problems.
- **Work with commands the CLI doesn't document.** Everything it knows comes from the CLI's own help output. If a parameter isn't described there, `azform` can't describe it either.
- **Cover the deep structure of generic update commands.** For things like `--set properties.encryption.keySource=...` you get a plain text field. The shape of a resource's properties isn't something the CLI exposes.
- **Undo anything.** It never changes anything in your subscription, so there's nothing to roll back — and once you press Enter, you're talking to Azure directly, same as always.

## Keyboard

The form is keyboard-only. Press **?** or **F1** inside it for an
overlay listing every binding in the current context.

```
↑  ↓   k  j       move between parameters
←  →   h  l       move between columns (grid layout)
Enter             edit field / open enum popup / confirm
Space             toggle optional parameter on/off (required fields show a hint)
Esc               close popup; from list, cancel and save draft
/                 filter by parameter name and help text
Tab  Shift-Tab    cycle list → Done → Cancel → list
g                 open a popup to set a shell variable (`name=value`, or just `name` to export the current session value) — writes the export into the calling shell on Done
G                 expand the Global Arguments section (the old `g` binding, shifted)
a                 show all collapsed parameters
v                 cycle value visibility for required params (see below)
Ctrl-G            select $VAR from the buffer list and insert at the cursor
w                 cycle through non-blocking warnings in the footer
```

`Enter` on **Done** is blocked while a blocking validation finding is
active — the footer names the parameter and the reason. `Esc` inside
a popup closes only the popup, not the whole form; cancelling the
selection does not lose what is already filled in.

### `v` — value visibility cycle

`v` is a global display toggle: every required parameter whose value
is a shell variable reference participates in the same cycle, and the
cycle is purely a view change — pressing `v` never mutates a field's
`Value`, `VarValue`, or `Mode`. The cycle advances through three
states in the value column:

```
state 0   $RG                       (just the var reference)
state 1   $RG → myResourceGroup     (default; reference and resolved value)
state 2   myResourceGroup           (just the resolved value)
```

After state 2, the next press wraps back to state 1, then 0, then 1,
and so on. Required fields whose reference does **not** resolve in
the current shell (e.g. you forgot to export `$RG`) stay red and
ignore the cycle — there is no resolved value to reveal. Optional
parameters and required parameters with a non-var literal value
(`"myResourceGroup"`, with no `$`) render their value normally and
also ignore the cycle.

The cycle works identically whether the variable reference came from
the env pre-fill, a remembered binding, the shell buffer
(`az … --resource-group $RG`), or a restored draft where the value
was saved as the literal text `$RG`. In all cases the resolved value
is looked up from the current shell session at render time; drafts
do not have to store a separate "this was a var" flag for the cycle
to apply.

Use `v` to preview what `az` will actually receive:

- state 0 answers "which variable did I bind this to?"
- state 1 is the default, useful while filling out the form
- state 2 answers "what literal value is about to be substituted?"

Pressing `v` while in text-edit mode types `v` into the input; press
`Esc` first to leave the field, then `v` to cycle.

## Privacy

`azform` runs entirely on your machine and holds nothing on a server.
The widget in your shell hands it a list of your current variables;
variables whose names match `*TOKEN*`, `*SECRET*`, `*KEY*`, `*PASSWORD*`,
`*PASSWD*`, or `*CREDENTIAL*` (case-insensitive) are filtered out
before anything is read from disk, so secrets never reach the form's
variable picker and never land in shell history through the form's
actions.

Drafts and bindings store *names* of variables, not resolved values —
`--resource-group $RG` is remembered as `RG`, never as the group name
itself. The only literal values persisted are enum parameters with a
closed value set (such as `--sku Standard_LRS`), where the alternative
would be to re-pick from the list each time.

`azform` does not handle Azure authentication in any form. Tokens,
device-code flows, and credential caches live entirely inside the
`az` binary the form calls. If `az` is signed out, the form degrades
gracefully — see *Diagnostics* below.

## Terminals

Reference platform is iTerm2 on macOS. The form needs the terminal to
emit standard escape sequences for arrow keys, Home, End, and Page
Up/Down; terminals that remap any of those — most commonly macOS
Terminal.app, which binds Home/End to scrollback by default — will
need them rebound to "beginning/end of line" for navigation to work.

Kitty, WezTerm, Alacritty, and plain xterm on Linux are expected to
work; VS Code's integrated terminal is not recommended because it
forwards keys inconsistently. If a key seems dead, the form's **?**
overlay lists every binding it understood, which usually identifies
the missing sequence at a glance.

---

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/someson/azform/main/install.sh | sh
```

One command. No `sudo`, nothing outside your home directory. The installer asks before touching your shell profile, and `install.sh --uninstall` removes everything it added.

Requires the Azure CLI to be installed and on your `PATH`. macOS and Linux for now.

## Update and uninstall

Update by rerunning the install command — the same one-liner, no
special path:

```sh
curl -fsSL https://raw.githubusercontent.com/someson/azform/main/install.sh | sh
```

Uninstall:

```sh
curl -fsSL https://raw.githubusercontent.com/someson/azform/main/install.sh | sh -s -- --uninstall
```

The binary and the widget are removed; your drafts, bindings, and
metadata cache are preserved. Add `--purge` if you want state gone
too. Re-running `install.sh` does not duplicate the widget block in
your shell profile — it checks for the markers first.

## Diagnostics

`azform --doctor` prints a summary of the runtime environment: which
`az` is found, where the cache and state directories are, whether
`az account get-access-token` works, and the relevant env-var
overrides. Run it first when something is wrong.

```sh
azform --doctor                  # environment summary
azform --version                 # azform version, commit, build date
azform --dump-cache "vm create"  # JSON view of cached metadata for one command
azform --parse-help save.txt     # parse a saved `az … --help` file, print the JSON
azform --debug                   # write structured events to <state-dir>/debug.log
```

State and cache live under standard XDG-style paths — plain JSON
files you can `cat` and delete:

| | macOS | Linux |
|---|---|---|
| cache | `~/Library/Caches/azform/` | `~/.cache/azform/` |
| state | `~/Library/Application Support/azform/` | `~/.local/state/azform/` |

The cache directory holds per-command metadata (`commands/*.json`).
The state directory holds `drafts.json` (form state from cancelled
forms, 20 entries, 7-day TTL), `bindings.json` (remembered
parameter-to-var links), and `parse-health.log` (rolling log of
parser self-diagnostics, last 200 entries). With `--debug`,
`debug.log` appears next to them.

If a parser change does not seem to take effect on a command you were
already editing, the cached metadata is the usual suspect — the form
will keep showing what was parsed last time. Delete the specific file
under `commands/` (or the whole cache directory) and reopen the form.

## Support me

`azform` is free, open source, and built in my own time. It will stay that way.

If it saves you a few trips to the documentation, here's what helps, in order of how much it actually matters:

- **Tell me what broke.** Bug reports with the command you were building are worth more than anything else on this list. The Azure CLI is large and its help output is not perfectly uniform — the parser will hit cases I never saw.
- **Tell me what's missing.** Especially if you gave up and typed the command by hand anyway. That's the most useful signal there is.
- **Star the repo** if you find it useful. It's how other people find it.

No paid tier, no telemetry, no account required. If that ever changes, it will be announced here first and the current feature set will remain free.

## Development

Requires Go 1.26+.

```sh
make build       # build ./bin/azform
make install     # go install into $HOME/.local/bin
make test        # go test ./...
make test-race   # go test -race ./...
make lint        # golangci-lint (requires `brew install golangci-lint`)
make lint-fix    # auto-fix goimports and other fixable issues
make help        # list all targets
```

Lint config lives in `.golangci.yml` and is tuned for signal-first. `make lint` must return `0 issues` before a PR is ready.

## License

MIT — see [LICENSE](LICENSE). Third-party license texts ship with each release.

---

*Built for the terminal. macOS and Linux first; Windows support is planned.*
