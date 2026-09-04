package render_test

import (
	"testing"

	"github.com/someson/azform/internal/render"
)

func TestEscapePOSIX(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Standard_LRS", "Standard_LRS"},
		{"StorageV2", "StorageV2"},
		{"TLS1_2", "TLS1_2"},
		{"true", "true"},
		{"false", "false"},
		{"my-group", "my-group"},
		{"my resource", "'my resource'"},
		{"val'ue", `'val'\''ue'`},
		{`val"ue`, `'val"ue'`},
		{"$RG", "'$RG'"},
		{"", "''"},
		{"with space and $var", `'with space and $var'`},
		{"glob*", "'glob*'"},
		{"a?b", "'a?b'"},
		{"a[b]", "'a[b]'"},
		{"a\\b", `'a\b'`},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := render.EscapePOSIX(tc.in)
			if got != tc.want {
				t.Errorf("EscapePOSIX(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBuild(t *testing.T) {
	cases := []struct {
		name string
		cmd  render.Command
		want string
	}{
		{
			name: "simple two flags",
			cmd: render.Command{
				Path: "group create",
				Fields: []render.FieldValue{
					{Name: "--name", Value: "my-group", Enabled: true},
					{Name: "--location", Value: "westeurope", Enabled: true},
				},
			},
			want: "az group create --name my-group --location westeurope",
		},
		{
			name: "disabled field skipped",
			cmd: render.Command{
				Path: "group create",
				Fields: []render.FieldValue{
					{Name: "--name", Value: "my-group", Enabled: true},
					{Name: "--tags", Value: "env=prod", Enabled: false},
				},
			},
			want: "az group create --name my-group",
		},
		{
			name: "var ref not escaped",
			cmd: render.Command{
				Path: "group create",
				Fields: []render.FieldValue{
					{Name: "--resource-group", Value: "$RG", IsVar: true, Enabled: true},
				},
			},
			want: "az group create --resource-group $RG",
		},
		{
			name: "value with space quoted",
			cmd: render.Command{
				Path: "group create",
				Fields: []render.FieldValue{
					{Name: "--name", Value: "my group", Enabled: true},
				},
			},
			want: "az group create --name 'my group'",
		},
		{
			name: "empty value skipped",
			cmd: render.Command{
				Path: "group create",
				Fields: []render.FieldValue{
					{Name: "--name", Value: "", Enabled: true},
					{Name: "--location", Value: "eastus", Enabled: true},
				},
			},
			want: "az group create --location eastus",
		},
		{
			name: "no args",
			cmd: render.Command{
				Path:   "group list",
				Fields: nil,
			},
			want: "az group list",
		},
		{
			name: "multiline explicit",
			cmd: render.Command{
				Path: "group create",
				Fields: []render.FieldValue{
					{Name: "--name", Value: "my-group", Enabled: true},
					{Name: "--location", Value: "westeurope", Enabled: true},
				},
				Multi: true,
			},
			want: "az group create \\\n  --name my-group \\\n  --location westeurope",
		},
		{
			name: "auto multiline when exceeds width",
			cmd: render.Command{
				Path: "storage account create",
				Fields: []render.FieldValue{
					{Name: "--name", Value: "mystorage", Enabled: true},
					{Name: "--resource-group", Value: "mygroup", Enabled: true},
				},
				Width: 40,
			},
			want: "az storage account create \\\n  --name mystorage \\\n  --resource-group mygroup",
		},
		{
			name: "switch emits bare flag, no value",
			cmd: render.Command{
				Path: "network application-gateway address-pool update",
				Fields: []render.FieldValue{
					{Name: "--debug", IsSwitch: true, Enabled: true},
					{Name: "--help", IsSwitch: true, Enabled: true},
					{Name: "--name", Value: "mypool", Enabled: true},
				},
			},
			want: "az network application-gateway address-pool update --debug --help --name mypool",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := render.Build(tc.cmd)
			if got != tc.want {
				t.Errorf("Build():\ngot  %q\nwant %q", got, tc.want)
			}
		})
	}
}
