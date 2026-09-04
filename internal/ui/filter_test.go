package ui_test

import (
	"testing"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/ui"
)

func makeField(name, help string) ui.Field {
	return ui.Field{Param: metadata.Parameter{Name: name, Help: help}}
}

func TestFilterFieldsEmpty(t *testing.T) {
	fields := []ui.Field{makeField("--name", "Name of the resource"), makeField("--location", "Azure region")}
	got := ui.FilterFields(fields, "")
	if len(got) != 2 {
		t.Errorf("empty query: got %d indices, want 2", len(got))
	}
}

func TestFilterFieldsByName(t *testing.T) {
	fields := []ui.Field{
		makeField("--name", "Resource name"),
		makeField("--location", "Azure region"),
		makeField("--resource-group", "Resource group name"),
	}
	got := ui.FilterFields(fields, "name")
	if len(got) != 2 {
		t.Errorf("query 'name': got %d, want 2 (--name, --resource-group)", len(got))
	}
}

func TestFilterFieldsByHelp(t *testing.T) {
	fields := []ui.Field{
		makeField("--sku", "The storage account SKU."),
		makeField("--kind", "Storage account kind."),
	}
	got := ui.FilterFields(fields, "storage")
	if len(got) != 2 {
		t.Errorf("query 'storage': got %d, want 2", len(got))
	}
}

func TestFilterFieldsCaseInsensitive(t *testing.T) {
	fields := []ui.Field{makeField("--name", "Resource name")}
	got := ui.FilterFields(fields, "NAME")
	if len(got) != 1 {
		t.Errorf("case-insensitive query: got %d, want 1", len(got))
	}
}

func TestFilterFieldsNoMatch(t *testing.T) {
	fields := []ui.Field{makeField("--name", "Resource name")}
	got := ui.FilterFields(fields, "xyzzy")
	if len(got) != 0 {
		t.Errorf("no-match query: got %d, want 0", len(got))
	}
}
