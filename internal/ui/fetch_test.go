package ui

import (
	"testing"
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
