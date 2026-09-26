package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
)

// stubAz replaces the az subprocess for one test and records every call.
func stubAz(t *testing.T, fn func(args []string) ([]byte, []byte, error)) *[]string {
	t.Helper()
	var calls []string
	prev := runAz
	runAz = func(_ context.Context, args ...string) ([]byte, []byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return fn(args)
	}
	t.Cleanup(func() { runAz = prev })
	return &calls
}

func TestImplicitSource(t *testing.T) {
	rg := metadata.Parameter{Name: "--resource-group", Aliases: []string{"-g"}}
	name := metadata.Parameter{Name: "--name", Aliases: []string{"--resource-group", "-g", "-n"}}
	cases := []struct {
		command string
		p       metadata.Parameter
		want    string
	}{
		{"vm create", rg, "az group list"},
		{"storage account show", rg, "az group list"},
		{"group show", name, "az group list"},
		{"group delete", name, "az group list"},
		// A new group is being named here: listing existing ones is wrong.
		{"group create", name, ""},
		{"vm create", metadata.Parameter{Name: "--location"}, ""},

		// --name of an existing resource.
		{"vm show", metadata.Parameter{Name: "--name"}, "az vm list"},
		{"vm start", metadata.Parameter{Name: "--name"}, "az vm list"},
		{"storage account keys list", metadata.Parameter{Name: "--name"}, "az storage account list"},
		{"aks get-credentials", metadata.Parameter{Name: "--name"}, "az aks list"},
		{"network bastion ssh", metadata.Parameter{Name: "--name"}, "az network bastion list"},
		// ... but never where --name is being created or is a child.
		{"vm create", metadata.Parameter{Name: "--name"}, ""},
		{"storage container create", metadata.Parameter{Name: "--name"}, ""},
		{"keyvault secret set", metadata.Parameter{Name: "--name"}, ""},
		{"network vnet subnet show", metadata.Parameter{Name: "--name"}, ""},
		{"sql db show", metadata.Parameter{Name: "--name"}, ""},

		// References to another existing resource.
		{"keyvault secret set", metadata.Parameter{Name: "--vault-name"}, "az keyvault list"},
		{"network vnet subnet create", metadata.Parameter{Name: "--vnet-name"}, "az network vnet list"},
		{"storage container create", metadata.Parameter{Name: "--account-name"}, "az storage account list"},
		{"sql db create", metadata.Parameter{Name: "--server"}, "az sql server list"},
		{"webapp create", metadata.Parameter{Name: "--plan"}, "az appservice plan list"},
		// Same param name, different meaning outside its family.
		{"cosmosdb sql database create", metadata.Parameter{Name: "--account-name"}, ""},
		{"postgres flexible-server create", metadata.Parameter{Name: "--server"}, ""},
	}
	for _, tc := range cases {
		if got := implicitSource(tc.command, tc.p); got != tc.want {
			t.Errorf("implicitSource(%q, %s) = %q, want %q", tc.command, tc.p.Name, got, tc.want)
		}
	}
}

func TestFetchCommandPassesSubscription(t *testing.T) {
	lookup := func(p string) string {
		if p == "--subscription" {
			return "sub-123"
		}
		return ""
	}
	got, ok := fetchCommand("az group list", lookup)
	if !ok || got != "group list --subscription sub-123" {
		t.Errorf("fetchCommand = %q, %v", got, ok)
	}
	if deps := fetchContextParams("az group list"); len(deps) != 1 || deps[0] != "--subscription" {
		t.Errorf("fetchContextParams = %v, want [--subscription]", deps)
	}
}

// Resource lists narrow to the form's group when one is set, and a change
// of group invalidates them.
func TestResourceListScopedByGroup(t *testing.T) {
	values := map[string]string{}
	lookup := func(p string) string { return values[p] }
	if got, _ := fetchCommand("az vm list", lookup); got != "vm list" {
		t.Errorf("without group: %q", got)
	}
	values["--resource-group"] = "prod-rg"
	if got, _ := fetchCommand("az vm list", lookup); got != "vm list --resource-group prod-rg" {
		t.Errorf("with group: %q", got)
	}
	if got, _ := fetchCommand("az group list", lookup); got != "group list" {
		t.Errorf("group list must not be scoped by a group: %q", got)
	}
	deps := strings.Join(fetchContextParams("az vm list"), ",")
	if deps != "--subscription,--resource-group" {
		t.Errorf("vm list deps = %q", deps)
	}
}

// vm show: the groups and, scoped to the chosen group, the VMs.
func TestVMNamesFollowResourceGroup(t *testing.T) {
	calls := stubAz(t, func(args []string) ([]byte, []byte, error) {
		if strings.Join(args[:2], " ") == "vm list" {
			return []byte(`[{"name":"web-2"},{"name":"web-1"}]`), nil, nil
		}
		return []byte(`[{"name":"prod-rg"}]`), nil, nil
	})
	f := NewForm("vm show", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, cmd := f.Update(MetadataLoadedMsg{Params: []metadata.Parameter{
		{Name: "--name", TakesValue: true, ValueKind: metadata.ValueKindString},
		{Name: "--resource-group", TakesValue: true, ValueKind: metadata.ValueKindString},
	}})
	f = m.(Form)
	for _, msg := range runCmd(cmd) {
		if fm, ok := msg.(FieldFetchedMsg); ok {
			m, _ = f.Update(fm)
			f = m.(Form)
		}
	}
	name := f.FieldIndex("--name")
	if got := strings.Join(f.fields[name].FetchedChoices, ","); got != "web-1,web-2" {
		t.Errorf("vm names = %q", got)
	}
	// Choosing a group drops the subscription-wide list and lists that
	// group's VMs right away.
	rg := f.FieldIndex("--resource-group")
	f.fields[rg].Value = "prod-rg"
	f.fields[rg].Enabled = true
	refetch := f.invalidateDependentFetches("--resource-group")
	if f.fields[name].FetchState != FetchLoading || f.fields[name].FetchedChoices != nil {
		t.Fatalf("vm names not refetched on group change: state=%v", f.fields[name].FetchState)
	}
	runCmd(refetch)
	if last := (*calls)[len(*calls)-1]; last != "vm list --resource-group prod-rg --output json" {
		t.Errorf("last az call = %q", last)
	}
}

// A fetch started before its context changed must not overwrite the list
// fetched for the new context.
func TestSupersededFetchResultIgnored(t *testing.T) {
	stubAz(t, func([]string) ([]byte, []byte, error) { return []byte(`[{"name":"x"}]`), nil, nil })
	f := NewForm("vm show", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, _ := f.Update(MetadataLoadedMsg{Params: []metadata.Parameter{
		{Name: "--name", TakesValue: true, ValueKind: metadata.ValueKindString},
		{Name: "--resource-group", TakesValue: true, ValueKind: metadata.ValueKindString},
	}})
	f = m.(Form)
	name := f.FieldIndex("--name")
	if f.fields[name].FetchState != FetchLoading {
		t.Fatalf("precondition: vm names should be prefetching")
	}
	old := f.fields[name].FetchGen
	f.invalidateDependentFetches("--resource-group")
	if f.fields[name].FetchGen == old {
		t.Fatalf("in-flight fetch not superseded")
	}
	m, _ = f.Update(FieldFetchedMsg{FieldIdx: name, Choices: []string{"stale-vm"}, Gen: old})
	f = m.(Form)
	if f.fields[name].FetchState != FetchLoading || f.fields[name].FetchedChoices != nil {
		t.Errorf("stale result applied: state=%v choices=%v", f.fields[name].FetchState, f.fields[name].FetchedChoices)
	}
	m, _ = f.Update(FieldFetchedMsg{FieldIdx: name, Choices: []string{"fresh-vm"}, Gen: f.fields[name].FetchGen})
	f = m.(Form)
	if got := strings.Join(f.fields[name].FetchedChoices, ","); got != "fresh-vm" {
		t.Errorf("current result not applied: %q", got)
	}
}

func TestFetchErrorMessages(t *testing.T) {
	if err := fetchError("group list", nil, exec.ErrNotFound); !strings.Contains(err.Error(), "az not found") {
		t.Errorf("missing az: %v", err)
	}
	stderr := []byte("ERROR: Please run 'az login' to setup account.\n")
	if err := fetchError("group list", stderr, errors.New("exit status 1")); !strings.Contains(err.Error(), "not signed in") {
		t.Errorf("not logged in: %v", err)
	}
	if err := fetchError("group list", []byte("ERROR: boom\nmore"), errors.New("exit 1")); err.Error() != "az group list: ERROR: boom more" {
		t.Errorf("generic: %v", err)
	}
}

func TestValuesCacheRoundTripAndTTL(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	saveCachedValues(dir, "group list", []string{"a", "b"}, now)
	if got, ok := loadCachedValues(dir, "group list", now.Add(time.Minute)); !ok || len(got) != 2 {
		t.Errorf("fresh entry: %v, %v", got, ok)
	}
	if _, ok := loadCachedValues(dir, "group list", now.Add(valuesCacheTTL+time.Second)); ok {
		t.Errorf("entry past TTL was served")
	}
	if _, ok := loadCachedValues(dir, "group list --subscription x", now); ok {
		t.Errorf("entry served for a different command")
	}
	if _, ok := loadCachedValues("", "group list", now); ok {
		t.Errorf("disabled cache served an entry")
	}
}

// `az account set` / `az login` rewrite azureProfile.json; values cached
// for the previous subscription must not be served afterwards.
func TestValuesCacheInvalidatedByProfileChange(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("AZURE_CONFIG_DIR", cfg)
	profile := cfg + "/azureProfile.json"
	if err := os.WriteFile(profile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	now := time.Now()
	saveCachedValues(dir, "group list", []string{"a"}, now)
	if _, ok := loadCachedValues(dir, "group list", now); !ok {
		t.Fatalf("fresh entry not served")
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(profile, later, later); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadCachedValues(dir, "group list", now); ok {
		t.Errorf("entry served after the Azure profile changed")
	}
}

// fetchField sorts resource lists and serves the second request from the
// on-disk cache without starting az again.
func TestFetchFieldSortsAndCaches(t *testing.T) {
	calls := stubAz(t, func([]string) ([]byte, []byte, error) {
		return []byte(`[{"name":"rg-b"},{"name":"rg-a"}]`), nil, nil
	})
	spec := fetchSpec{command: "group list", sorted: true, cacheDir: t.TempDir()}
	msg := fetchField(3, spec)().(FieldFetchedMsg)
	if msg.Err != nil || strings.Join(msg.Choices, ",") != "rg-a,rg-b" || msg.FieldIdx != 3 {
		t.Fatalf("first fetch: %+v", msg)
	}
	msg = fetchField(3, spec)().(FieldFetchedMsg)
	if strings.Join(msg.Choices, ",") != "rg-a,rg-b" {
		t.Fatalf("cached fetch: %+v", msg)
	}
	if len(*calls) != 1 || (*calls)[0] != "group list --output json" {
		t.Errorf("az calls = %v, want exactly one", *calls)
	}
}

// Opening a form with --resource-group starts `az group list` right away;
// by the time the user presses Enter on the field, the groups are offered.
func TestResourceGroupsPrefetchedAndPickable(t *testing.T) {
	calls := stubAz(t, func([]string) ([]byte, []byte, error) {
		return []byte(`[{"name":"prod-rg"},{"name":"dev-rg"}]`), nil, nil
	})
	// vm create: --name is a new VM, so only the groups are listed.
	f := NewForm("vm create", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, cmd := f.Update(MetadataLoadedMsg{Params: []metadata.Parameter{
		{Name: "--name", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString},
		{Name: "--resource-group", Required: true, TakesValue: true, ValueKind: metadata.ValueKindString},
	}})
	f = m.(Form)
	rg := f.FieldIndex("--resource-group")
	if f.fields[rg].FetchState != FetchLoading {
		t.Fatalf("--resource-group not prefetched: state=%v", f.fields[rg].FetchState)
	}
	for _, msg := range runCmd(cmd) {
		if fm, ok := msg.(FieldFetchedMsg); ok {
			m, _ = f.Update(fm)
			f = m.(Form)
		}
	}
	if got := strings.Join(f.fields[rg].FetchedChoices, ","); got != "dev-rg,prod-rg" {
		t.Fatalf("choices = %q (calls %v)", got, *calls)
	}
	if n := len(*calls); n != 1 {
		t.Errorf("az ran %d times, want 1 (--name of vm create has no source)", n)
	}

	// Move to --resource-group, open the picker, pick the first group.
	f.cursor = 1
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(Form)
	if f.mode != FormModeEnum {
		t.Fatalf("Enter should open the picker, mode=%v", f.mode)
	}
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyDown})
	f = m.(Form)
	m, pick := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(Form)
	m, _ = f.Update(pick())
	f = m.(Form)
	if got := f.fields[rg].Value; got != "dev-rg" {
		t.Errorf("picked %q, want dev-rg", got)
	}
}

// Changing --subscription drops groups listed for the previous one.
func TestSubscriptionEditInvalidatesResourceGroups(t *testing.T) {
	f := NewForm("vm show", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, _ := f.Update(MetadataLoadedMsg{Params: []metadata.Parameter{
		{Name: "--resource-group", TakesValue: true, ValueKind: metadata.ValueKindString},
		{Name: "--subscription", TakesValue: true, ValueKind: metadata.ValueKindString, Global: true},
	}})
	f = m.(Form)
	rg := f.FieldIndex("--resource-group")
	f.fields[rg].FetchState = FetchLoaded
	f.fields[rg].FetchedChoices = []string{"old-rg"}
	stubAz(t, func([]string) ([]byte, []byte, error) { return []byte(`[]`), nil, nil })
	if cmd := f.invalidateDependentFetches("--subscription"); cmd == nil {
		t.Errorf("no refetch scheduled")
	}
	if f.fields[rg].FetchState != FetchLoading || f.fields[rg].FetchedChoices != nil {
		t.Errorf("groups not refetched after --subscription change: state=%v", f.fields[rg].FetchState)
	}
}

// Grid cells have no room for "(N options)": the cell shows ▼ / ! and the
// footer names the count or the error for the focused field.
func TestGridShowsFetchState(t *testing.T) {
	f := NewForm("vm show", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, _ := f.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m, _ = m.(Form).Update(MetadataLoadedMsg{Params: []metadata.Parameter{
		{Name: "--resource-group", TakesValue: true, ValueKind: metadata.ValueKindString},
		{Name: "--output", TakesValue: true, ValueKind: metadata.ValueKindString, Global: true},
	}})
	f = m.(Form)
	if _, _, cols := f.gridLayout(); cols < 2 {
		t.Fatalf("expected grid layout, got %d column(s)", cols)
	}
	rg := f.FieldIndex("--resource-group")
	f.cursor = 0
	f.fields[rg].FetchState = FetchLoaded
	f.fields[rg].FetchedChoices = []string{"a", "b", "c"}
	if note := stripANSI(f.gridFetchNote()); note != "--resource-group: 3 values — Enter to pick" {
		t.Errorf("loaded note = %q", note)
	}
	if cell := stripANSI(f.renderGridCell(rg, true, 16)); !strings.Contains(cell, "▼") {
		t.Errorf("loaded cell lacks ▼: %q", cell)
	}
	f.fields[rg].FetchState = FetchError
	f.fields[rg].FetchError = "not signed in (run az login) — type a value"
	if note := stripANSI(f.gridFetchNote()); !strings.Contains(note, "not signed in") {
		t.Errorf("error note = %q", note)
	}
	if cell := stripANSI(f.renderGridCell(rg, true, 16)); !strings.Contains(cell, "!") {
		t.Errorf("error cell lacks !: %q", cell)
	}
}
