package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/debug"
	"github.com/someson/azform/internal/lock"
	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/shell"
	"github.com/someson/azform/internal/state"
	"github.com/someson/azform/internal/term"
	"github.com/someson/azform/internal/ui"
	"github.com/someson/azform/internal/update"
	"github.com/someson/azform/internal/validate"
	"github.com/someson/azform/internal/vars"
)

var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("azform", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var (
		showVersion   bool
		line          string
		outPath       string
		cursor        int
		varsPath      string
		cwd           string
		cacheDir      string
		stateDir      string
		dumpCache     string
		parseHelp     string
		noUpdateCheck bool
		debugFlag     bool
		doctorFlag    bool
	)
	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	fs.StringVar(&line, "line", "", "current shell buffer contents")
	fs.StringVar(&outPath, "out", "", "file path to write the assembled command")
	fs.IntVar(&cursor, "cursor", 0, "cursor position in --line")
	fs.StringVar(&varsPath, "vars", "", "NUL-separated NAME=VALUE file from the shell widget")
	fs.StringVar(&cwd, "cwd", "", "shell working directory (for @ path completion)")
	fs.StringVar(&cacheDir, "cache-dir", "", "override metadata cache directory")
	fs.StringVar(&stateDir, "state-dir", "", "override state directory (drafts, bindings)")
	fs.StringVar(&dumpCache, "dump-cache", "", "print cached metadata for an Azure CLI command path and exit")
	fs.StringVar(&parseHelp, "parse-help", "", "parse a saved az --help output file and print the result")
	fs.BoolVar(&noUpdateCheck, "no-update-check", false, "skip self-update check (also via AZFORM_NO_UPDATE_CHECK=1)")
	fs.BoolVar(&debugFlag, "debug", false, "write structured debug events to <state-dir>/debug.log (spec §15.3)")
	fs.BoolVar(&doctorFlag, "doctor", false, "print environment summary and exit (spec §15.3)")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: azform --line <buffer> --out <path> [--vars <path>] [--cwd <path>]\n\n")
		fmt.Fprintf(fs.Output(), "Flags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --no-update-check propagates to the env var that update.Check checks.
	if noUpdateCheck {
		_ = os.Setenv("AZFORM_NO_UPDATE_CHECK", "1")
	}

	// cwd is consumed by the @-path completion stage; suppress unused.
	_ = cwd

	if showVersion {
		printVersion()
		return 0
	}
	if doctorFlag {
		return runDoctor(ctx, os.Stdout, doctorOptions{
			version:  version,
			commit:   commit,
			date:     date,
			cacheDir: effectiveCacheDir(cacheDir),
			stateDir: effectiveStateDir(stateDir),
		})
	}

	if stateDir == "" {
		stateDir = state.DefaultStateDir()
	}

	var dbg *debug.Logger
	if debugFlag {
		l, err := debug.Open(stateDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "azform: open debug log: %v\n", err)
			return 2
		}
		defer func() { _ = l.Close() }()
		dbg = l
	}

	if parseHelp != "" {
		return runParseHelp(parseHelp)
	}
	if dumpCache != "" {
		return runDumpCache(ctx, dumpCache, cacheDir, dbg)
	}

	// Parse the shell buffer to locate the az command.
	raw, ok := shell.ParseRaw(line, cursor)
	if !ok {
		if len(fs.Args()) == 0 {
			fs.Usage()
			return 2
		}
		raw = shell.RawBuffer{
			CommandPath: strings.Join(fs.Args(), " "),
		}
	}

	if raw.CommandPath == "" {
		fmt.Fprintln(os.Stderr, "azform: could not determine az command from --line or positional args")
		return 2
	}
	if outPath == "" {
		fmt.Fprintln(os.Stderr, "azform: --out is required for TUI mode")
		return 2
	}

	// Load shell vars from the widget's file. A widget that doesn't dump vars
	// (e.g. bash `bind -x` with no dump helper) silently produces an empty list.
	var shellVars []vars.Variable
	if varsPath != "" {
		if v, err := vars.ReadFile(varsPath); err == nil {
			shellVars = v
		}
	}
	azureDefaults := vars.LoadAzureDefaults()

	return runTUI(raw, shellVars, azureDefaults, outPath, stateDir, cacheDir, dbg)
}

func runTUI(raw shell.RawBuffer, shellVars, azureDefaults []vars.Variable, outPath, stateDir, cacheDir string, dbg *debug.Logger) int {
	cache := metadata.NewCache(cacheDir, version, nil)
	cache.Debug = dbg
	sessionNames := make([]string, 0, len(shellVars))
	for _, v := range shellVars {
		sessionNames = append(sessionNames, v.Name)
	}
	bindings := state.NewBindingsStore(stateDir)
	src := ui.Sources{
		Buffer:        raw,
		Vars:          shellVars,
		AzureDefaults: azureDefaults,
		Engine:        validate.NewEngine(validate.BuiltinProvider{}),
		SessionVars:   sessionNames,
		Bindings:      bindings,
		Debug:         dbg,
	}
	src.UpdateCheck = update.CheckCmd(update.Options{
		Repo:     "someson/azform",
		Current:  version,
		StateDir: stateDir,
	})
	form := ui.NewFormWithSources(raw.CommandPath, outPath, stateDir, version, cache, src)

	tty, err := term.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "azform: open terminal: %v\n", err)
		return 2
	}
	defer func() { _ = tty.Close() }()

	lk, err := lock.Acquire(tty)
	if errors.Is(err, lock.ErrLocked) {
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "azform: lock: %v\n", err)
		return 2
	}
	defer func() { _ = lk.Close() }()

	p := tea.NewProgram(form,
		tea.WithInput(tty),
		tea.WithOutput(tty),
		// WithAltScreen swaps to a separate screen on Init and back to the
		// shell on Quit, so the widget fills the whole window without residue
		// from the previous buffer. Without it, in-place renders can leave
		// the old shell prompt above the form.
		tea.WithAltScreen(),
	)
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "azform: TUI error: %v\n", err)
		return 2
	}

	f := finalModel.(ui.Form)
	result := f.Result()
	if result == "" {
		return 1
	}

	declPrefix := ""
	for _, d := range f.Declarations() {
		declPrefix += d.Name + "=" + d.Value + " && "
	}

	output := raw.Prefix + declPrefix + result + raw.Suffix
	if err := os.WriteFile(outPath, []byte(output+"\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "azform: write result: %v\n", err)
		return 2
	}
	return 0
}

func printVersion() {
	fmt.Printf("azform %s", version)
	if commit != "" {
		fmt.Printf(" (%s", commit)
		if date != "" {
			fmt.Printf(", %s", date)
		}
		fmt.Printf(")")
	}
	fmt.Println()
}

func effectiveCacheDir(override string) string {
	if override != "" {
		return override
	}
	return metadata.DefaultCacheDir()
}

func effectiveStateDir(override string) string {
	if override != "" {
		return override
	}
	return state.DefaultStateDir()
}

func runParseHelp(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "azform: read %s: %v\n", path, err)
		return 1
	}
	doc, err := metadata.Parse(string(data))
	if err != nil {
		fmt.Fprintf(os.Stderr, "azform: parse %s: %v\n", path, err)
		return 1
	}
	return printJSON(doc)
}

func runDumpCache(ctx context.Context, commandPath, cacheDir string, dbg *debug.Logger) int {
	cache := metadata.NewCache(cacheDir, version, nil)
	cache.Debug = dbg
	result, err := cache.Resolve(ctx, commandPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "azform: %v\n", err)
		return 1
	}
	if result.Stale {
		fmt.Fprintln(os.Stderr, "azform: cache entry is stale")
	}
	if result.Command != nil {
		return printJSON(result.Command)
	}
	return printJSON(result.Group)
}

func printJSON(value any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fmt.Fprintf(os.Stderr, "azform: encode JSON: %v\n", err)
		return 1
	}
	return 0
}
