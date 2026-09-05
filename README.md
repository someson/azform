# azform

**A form for building Azure CLI commands, right in your terminal.**

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
- Turn fixed value sets into pickable lists, so misspellings stop happening
- Pick up a command you already started typing and let you finish it in the form
- Fill fields with shell variables you already have defined, and remember which variable you used for which parameter
- Warn you before you run a command that references a variable your shell doesn't actually have
- Show live values from your Azure subscription where it makes sense — resource groups, locations, existing resources
- Save named presets, so "a storage account like the one in project X" is one keystroke
- Give the finished command back as a single line, as a multi-line script block, or on your clipboard

## What it can't do

- **Tell you what to build.** It shows you the parameters; deciding what belongs in them is your job.
- **Validate against Azure before you run.** Some things are only knowable by trying. It catches missing required parameters, wrong enum values, and undefined variables — not quota limits, naming conflicts, or permission problems.
- **Work with commands the CLI doesn't document.** Everything it knows comes from the CLI's own help output. If a parameter isn't described there, `azform` can't describe it either.
- **Cover the deep structure of generic update commands.** For things like `--set properties.encryption.keySource=...` you get a plain text field. The shape of a resource's properties isn't something the CLI exposes.
- **Undo anything.** It never changes anything in your subscription, so there's nothing to roll back — and once you press Enter, you're talking to Azure directly, same as always.

---

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/someson/azform/main/install.sh | sh
```

One command. No `sudo`, nothing outside your home directory. The installer asks before touching your shell profile, and `install.sh --uninstall` removes everything it added.

Requires the Azure CLI to be installed and on your `PATH`. macOS and Linux for now.

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
