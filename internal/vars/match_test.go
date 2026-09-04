package vars_test

import (
	"reflect"
	"testing"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/vars"
)

var matchParams = []metadata.Parameter{
	{Name: "--name", Aliases: []string{"-n"}, TakesValue: true, ValueKind: metadata.ValueKindString},
	{Name: "--resource-group", Aliases: []string{"-g"}, TakesValue: true, ValueKind: metadata.ValueKindString},
	{Name: "--location", Aliases: []string{"-l"}, TakesValue: true, ValueKind: metadata.ValueKindString},
	{Name: "--sku", Aliases: nil, TakesValue: true, ValueKind: metadata.ValueKindEnum, Choices: []string{"Standard_LRS", "Premium_LRS"}},
	{Name: "--tier", Aliases: nil, TakesValue: true, ValueKind: metadata.ValueKindEnum, Choices: []string{"Standard_LRS", "Premium_LRS"}},
}

func TestMatchExactShort(t *testing.T) {
	in := []vars.Variable{{Name: "RG", Value: "my-group"}}
	got := vars.MatchVariables(in, matchParams)
	if len(got) != 1 || got[0].ParamName != "--resource-group" || got[0].VarName != "RG" {
		t.Errorf("got %+v, want one match on --resource-group from RG", got)
	}
}

func TestMatchNormalisedUnderscore(t *testing.T) {
	in := []vars.Variable{{Name: "RESOURCE_GROUP", Value: "rg1"}, {Name: "resource-group", Value: "rg2"}}
	got := vars.MatchVariables(in, matchParams)
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1 (RG only binds once)", len(got))
	}
	if got[0].ParamName != "--resource-group" {
		t.Errorf("got[0].ParamName = %q, want --resource-group", got[0].ParamName)
	}
}

func TestMatchAzureDefaultsPrefix(t *testing.T) {
	in := []vars.Variable{{Name: "AZURE_DEFAULTS_LOCATION", Value: "westeurope"}}
	got := vars.MatchVariables(in, matchParams)
	if len(got) != 1 || got[0].ParamName != "--location" {
		t.Errorf("got %+v, want match on --location", got)
	}
}

func TestMatchNameSuffix(t *testing.T) {
	in := []vars.Variable{{Name: "STORAGE_ACCOUNT_NAME", Value: "mystorage"}}
	got := vars.MatchVariables(in, matchParams)
	if len(got) != 1 || got[0].ParamName != "--name" {
		t.Errorf("got %+v, want --name (suffix _NAME)", got)
	}
}

func TestMatchValueSignal(t *testing.T) {
	// Unusual var name, but value matches --sku's choices.
	in := []vars.Variable{{Name: "WEIRD_VAR", Value: "Standard_LRS"}}
	got := vars.MatchVariables(in, matchParams)
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1 (value signal)", len(got))
	}
	if got[0].ParamName != "--sku" && got[0].ParamName != "--tier" {
		t.Errorf("got[0].ParamName = %q, want --sku or --tier", got[0].ParamName)
	}
}

func TestMatchValueSignalSkipsBool(t *testing.T) {
	// Bool params share choices {false,true,...} with countless unrelated vars
	// (CI=true, DEBUG=false); the value signal must never bind to them.
	params := append([]metadata.Parameter{}, matchParams...)
	params = append(params, metadata.Parameter{
		Name: "--allow-blob-public-access", TakesValue: true,
		ValueKind: metadata.ValueKindBool, Choices: []string{"false", "true"},
	})
	in := []vars.Variable{{Name: "CI", Value: "true"}}
	got := vars.MatchVariables(in, params)
	if len(got) != 0 {
		t.Errorf("bool param should not match by value, got %+v", got)
	}
	// Enum params still match by value.
	in = []vars.Variable{{Name: "WEIRD", Value: "Standard_LRS"}}
	got = vars.MatchVariables(in, params)
	if len(got) != 1 {
		t.Errorf("enum param should still match by value, got %+v", got)
	}
}

func TestMatchNoDuplicates(t *testing.T) {
	// Two vars both matching --resource-group: only the first wins.
	in := []vars.Variable{
		{Name: "RG", Value: "first"},
		{Name: "RESOURCE_GROUP", Value: "second"},
	}
	got := vars.MatchVariables(in, matchParams)
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1", len(got))
	}
	if got[0].Value != "first" {
		t.Errorf("got[0].Value = %q, want %q (first wins)", got[0].Value, "first")
	}
}

func TestMatchEmpty(t *testing.T) {
	got := vars.MatchVariables(nil, matchParams)
	if len(got) != 0 {
		t.Errorf("got %+v, want empty", got)
	}
}

func TestMatchValueSignalPrefersName(t *testing.T) {
	// Both name and value signal match; name should win for ordering stability.
	in := []vars.Variable{
		{Name: "SKU", Value: "Standard_LRS"},
		{Name: "TIER", Value: "Premium_LRS"},
	}
	got := vars.MatchVariables(in, matchParams)
	names := []string{}
	for _, m := range got {
		names = append(names, m.ParamName)
	}
	want := []string{"--sku", "--tier"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got order %v, want %v", names, want)
	}
}

func TestMatchSkipsSensitive(t *testing.T) {
	// ReadFile already filters, but MatchVariables is the second line of defence.
	in := []vars.Variable{{Name: "AZURE_CLIENT_SECRET", Value: "shh"}}
	got := vars.MatchVariables(in, matchParams)
	if len(got) != 0 {
		t.Errorf("sensitive var should be skipped, got %+v", got)
	}
}
