package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/someson/azform/internal/metadata"
)

func TestBuildFetchArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"az account list-locations", []string{"account", "list-locations", "--output", "json"}},
		{"az account list-locations --output json", []string{"account", "list-locations", "--output", "json"}},
		{"az account list -o json", []string{"account", "list", "-o", "json"}},
		{"az account list --output=json", []string{"account", "list", "--output=json"}},
		{"az account list -o=json", []string{"account", "list", "-o=json"}},
		{"   az    account   list-locations   ", []string{"account", "list-locations", "--output", "json"}},
	}
	for _, tc := range cases {
		got := buildFetchArgs(tc.in)
		if !equalStrings(got, tc.want) {
			t.Errorf("buildFetchArgs(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseFetchedValuesArray(t *testing.T) {
	raw := []byte(`[{"name":"eastus"},{"name":"westus"},{"name":"northeurope"}]`)
	got, err := parseFetchedValues(raw)
	if err != nil {
		t.Fatalf("parseFetchedValues: %v", err)
	}
	want := []string{"eastus", "westus", "northeurope"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseFetchedValuesDisplayNameFallback(t *testing.T) {
	raw := []byte(`[{"displayName":"East US","name":"eastus"},{"displayName":"West US","name":"westus"}]`)
	got, err := parseFetchedValues(raw)
	if err != nil {
		t.Fatalf("parseFetchedValues: %v", err)
	}
	// `name` wins over `displayName` per the §4.5 heuristic.
	want := []string{"eastus", "westus"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseFetchedValuesNoNameField(t *testing.T) {
	// No `name` and no `displayName` → fall back to first non-empty string.
	raw := []byte(`[{"foo":"x"},{"bar":"y"}]`)
	got, err := parseFetchedValues(raw)
	if err != nil {
		t.Fatalf("parseFetchedValues: %v", err)
	}
	want := []string{"x", "y"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseFetchedValuesObjectWrapper(t *testing.T) {
	raw := []byte(`{"value":[{"name":"a"},{"name":"b"}]}`)
	got, err := parseFetchedValues(raw)
	if err != nil {
		t.Fatalf("parseFetchedValues: %v", err)
	}
	want := []string{"a", "b"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseFetchedValuesEmptyArray(t *testing.T) {
	if _, err := parseFetchedValues([]byte(`[]`)); err == nil {
		t.Errorf("expected error for empty array")
	}
}

func TestParseFetchedValuesNoArray(t *testing.T) {
	if _, err := parseFetchedValues([]byte(`"hello"`)); err == nil {
		t.Errorf("expected error for non-array response")
	}
}

func TestParseFetchedValuesInvalidJSON(t *testing.T) {
	if _, err := parseFetchedValues([]byte(`{`)); err == nil {
		t.Errorf("expected error for invalid JSON")
	}
}

func TestParseFetchedValuesSkipsNonObjects(t *testing.T) {
	// `az ... --output json` may include null or scalar entries in mixed
	// arrays; only the object entries contribute.
	raw := []byte(`[{"name":"a"},null,"str",{"name":"b"}]`)
	got, err := parseFetchedValues(raw)
	if err != nil {
		t.Fatalf("parseFetchedValues: %v", err)
	}
	want := []string{"a", "b"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestOneLine(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello\nworld", "hello world"},
		{"a\rb\rc", "a b c"},
		{"   trimmed   ", "trimmed"},
		{"x", "x"},
	}
	for _, tc := range cases {
		if got := oneLine(tc.in); got != tc.want {
			t.Errorf("oneLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFetchCommand(t *testing.T) {
	values := map[string]string{"--location": "westeurope", "--resource-group": "rg", "--name": "aks1"}
	lookup := func(p string) string { return values[p] }
	empty := func(string) string { return "" }
	cases := []struct {
		vf     string
		lookup func(string) string
		want   string
		ok     bool
	}{
		{"az account list-locations", lookup, "account list-locations", true},
		{"`az account list-locations`.", lookup, "account list-locations", true},
		{"az vm image list, az vm image show, az sig image-version show-shared", lookup, "vm image list", true},
		{"az vm list-sizes", lookup, "vm list-sizes --location westeurope", true},
		{"az vm list-sizes", empty, "", false},
		{"az aks get-upgrades", lookup, "aks get-upgrades --resource-group rg --name aks1", true},
		{"az foo show --name <name>", lookup, "", false},
		{"", lookup, "", false},
	}
	for _, tc := range cases {
		got, ok := fetchCommand(tc.vf, tc.lookup)
		if got != tc.want || ok != tc.ok {
			t.Errorf("fetchCommand(%q) = %q, %v; want %q, %v", tc.vf, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseFetchedValuesVersionsAndStrings(t *testing.T) {
	// aks get-versions: object wrapper, items keyed by "version".
	got, err := parseFetchedValues([]byte(`{"id":"x","name":"default","values":[{"version":"1.30","isPreview":false},{"version":"1.29"}]}`))
	if err != nil || !equalStrings(got, []string{"1.30", "1.29"}) {
		t.Errorf("versions: got %v, %v", got, err)
	}
	// aks get-upgrades: nested object, items keyed by kubernetesVersion.
	got, err = parseFetchedValues([]byte(`{"agentPoolProfiles":null,"controlPlaneProfile":{"upgrades":[{"kubernetesVersion":"1.31.1"}]}}`))
	if err != nil || !equalStrings(got, []string{"1.31.1"}) {
		t.Errorf("upgrades: got %v, %v", got, err)
	}
	// Plain string array, duplicates dropped.
	got, err = parseFetchedValues([]byte(`["a","b","a"]`))
	if err != nil || !equalStrings(got, []string{"a", "b"}) {
		t.Errorf("strings: got %v, %v", got, err)
	}
}

// Fetched values used to be shown only as "(N options)"; Enter now offers
// them in the picker, whose first row falls back to free text.
func TestFetchedChoicesArePickable(t *testing.T) {
	vf := "az account list-locations"
	f := NewForm("group create", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, _ := f.Update(MetadataLoadedMsg{Params: []metadata.Parameter{
		{Name: "--location", TakesValue: true, ValueKind: metadata.ValueKindString, ValuesFrom: &vf},
	}})
	f = m.(Form)
	idx := f.FieldIndex("--location")
	f.fields[idx].FetchState = FetchLoaded
	f.fields[idx].FetchedChoices = []string{"westeurope", "northeurope"}

	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(Form)
	if f.mode != FormModeEnum {
		t.Fatalf("Enter should open the picker, mode=%v", f.mode)
	}
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyDown})
	f = m.(Form)
	m, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(Form)
	m, _ = f.Update(cmd())
	f = m.(Form)
	if got := f.fields[idx].Value; got != "westeurope" {
		t.Errorf("picked value = %q, want westeurope", got)
	}

	// First row: free-text entry.
	m, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(Form)
	for f.enumPop.cursor > 0 {
		m, _ = f.Update(tea.KeyMsg{Type: tea.KeyUp})
		f = m.(Form)
	}
	m, cmd = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = m.(Form)
	m, _ = f.Update(cmd())
	f = m.(Form)
	if f.mode != FormModeEdit {
		t.Errorf("manual-entry row should open the editor, mode=%v", f.mode)
	}
}

// Editing a context param throws away choices fetched for its old value.
func TestContextEditInvalidatesFetchedChoices(t *testing.T) {
	vf := "az vm list-sizes"
	f := NewForm("vm create", "/tmp/out.txt", t.TempDir(), "test", nil)
	m, _ := f.Update(MetadataLoadedMsg{Params: []metadata.Parameter{
		{Name: "--location", TakesValue: true, ValueKind: metadata.ValueKindString},
		{Name: "--size", TakesValue: true, ValueKind: metadata.ValueKindString, ValuesFrom: &vf},
	}})
	f = m.(Form)
	size := f.FieldIndex("--size")
	f.fields[size].FetchState = FetchLoaded
	f.fields[size].FetchedChoices = []string{"Standard_B1s"}
	if cmd := f.maybeFetchField(size); cmd != nil {
		t.Fatalf("loaded field must not refetch")
	}
	f.invalidateDependentFetches("--location")
	if f.fields[size].FetchState != FetchIdle || f.fields[size].FetchedChoices != nil {
		t.Errorf("choices not invalidated: state=%v", f.fields[size].FetchState)
	}
	// With --location still empty, nothing is run.
	if cmd := f.maybeFetchField(size); cmd != nil || f.fields[size].FetchState != FetchIdle {
		t.Errorf("fetch without --location should not start")
	}
}
