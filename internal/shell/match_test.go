package shell_test

import (
	"testing"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/shell"
)

// testParams mirrors a subset of az storage account create parameters.
var testParams = []metadata.Parameter{
	{Name: "--name", Aliases: []string{"-n"}, TakesValue: true, ValueKind: metadata.ValueKindString},
	{Name: "--resource-group", Aliases: []string{"-g"}, TakesValue: true, ValueKind: metadata.ValueKindString},
	{Name: "--location", Aliases: []string{"-l"}, TakesValue: true, ValueKind: metadata.ValueKindString},
	{Name: "--sku", Aliases: nil, TakesValue: true, ValueKind: metadata.ValueKindEnum, Choices: []string{"Standard_LRS", "Premium_LRS"}},
	{Name: "--enable-https-traffic-only", Aliases: nil, TakesValue: false, ValueKind: metadata.ValueKindBool},
	{Name: "--tags", Aliases: nil, TakesValue: true, ValueKind: metadata.ValueKindKeyValue},
}

func rawFor(t *testing.T, line string) shell.RawBuffer {
	t.Helper()
	raw, ok := shell.ParseRaw(line, 0)
	if !ok {
		t.Fatalf("ParseRaw(%q) returned false", line)
	}
	return raw
}

func TestMatchParamsSimple(t *testing.T) {
	raw := rawFor(t, "az storage account create --name mystorage --resource-group mygroup")
	parsed := shell.MatchParams(raw, testParams)
	if len(parsed.Params) != 2 {
		t.Fatalf("Params count = %d, want 2", len(parsed.Params))
	}
	if parsed.Params[0].Flag != "--name" {
		t.Errorf("Params[0].Flag = %q, want \"--name\"", parsed.Params[0].Flag)
	}
	if parsed.Params[0].Value != "mystorage" {
		t.Errorf("Params[0].Value = %q, want \"mystorage\"", parsed.Params[0].Value)
	}
	if parsed.Params[1].Flag != "--resource-group" {
		t.Errorf("Params[1].Flag = %q, want \"--resource-group\"", parsed.Params[1].Flag)
	}
	if parsed.Params[1].Value != "mygroup" {
		t.Errorf("Params[1].Value = %q, want \"mygroup\"", parsed.Params[1].Value)
	}
}

func TestMatchParamsShortAlias(t *testing.T) {
	raw := rawFor(t, "az group create -g mygroup")
	parsed := shell.MatchParams(raw, testParams)
	if len(parsed.Params) != 1 {
		t.Fatalf("Params count = %d, want 1", len(parsed.Params))
	}
	if parsed.Params[0].Flag != "--resource-group" {
		t.Errorf("Flag = %q, want \"--resource-group\" (canonicalized from -g)", parsed.Params[0].Flag)
	}
	if parsed.Params[0].RawFlag != "-g" {
		t.Errorf("RawFlag = %q, want \"-g\"", parsed.Params[0].RawFlag)
	}
	if parsed.Params[0].Value != "mygroup" {
		t.Errorf("Value = %q, want \"mygroup\"", parsed.Params[0].Value)
	}
}

func TestMatchParamsEqualsSign(t *testing.T) {
	raw := rawFor(t, "az group create --name=my-group")
	parsed := shell.MatchParams(raw, testParams)
	if len(parsed.Params) != 1 {
		t.Fatalf("Params count = %d, want 1", len(parsed.Params))
	}
	if parsed.Params[0].Flag != "--name" {
		t.Errorf("Flag = %q, want \"--name\"", parsed.Params[0].Flag)
	}
	if parsed.Params[0].Value != "my-group" {
		t.Errorf("Value = %q, want \"my-group\"", parsed.Params[0].Value)
	}
}

func TestMatchParamsVarRef(t *testing.T) {
	raw := rawFor(t, "az group create --resource-group $RG")
	parsed := shell.MatchParams(raw, testParams)
	if len(parsed.Params) != 1 {
		t.Fatalf("Params count = %d, want 1", len(parsed.Params))
	}
	p := parsed.Params[0]
	if !p.IsVar {
		t.Error("IsVar should be true for $RG")
	}
	if p.VarName != "RG" {
		t.Errorf("VarName = %q, want \"RG\"", p.VarName)
	}
	if p.Value != "$RG" {
		t.Errorf("Value = %q, want \"$RG\"", p.Value)
	}
}

func TestMatchParamsVarRefBraces(t *testing.T) {
	raw := rawFor(t, "az group create --resource-group ${RESOURCE_GROUP}")
	parsed := shell.MatchParams(raw, testParams)
	p := parsed.Params[0]
	if !p.IsVar {
		t.Error("IsVar should be true for ${RESOURCE_GROUP}")
	}
	if p.VarName != "RESOURCE_GROUP" {
		t.Errorf("VarName = %q, want \"RESOURCE_GROUP\"", p.VarName)
	}
}

func TestMatchParamsBoolFlag(t *testing.T) {
	raw := rawFor(t, "az storage account create --enable-https-traffic-only")
	parsed := shell.MatchParams(raw, testParams)
	if len(parsed.Params) != 1 {
		t.Fatalf("Params count = %d, want 1", len(parsed.Params))
	}
	p := parsed.Params[0]
	if p.Flag != "--enable-https-traffic-only" {
		t.Errorf("Flag = %q", p.Flag)
	}
	if p.Value != "true" {
		t.Errorf("Value = %q, want \"true\" for bare bool flag", p.Value)
	}
	if p.Unknown {
		t.Error("Unknown should be false — param is in metadata")
	}
}

func TestMatchParamsUnknown(t *testing.T) {
	raw := rawFor(t, "az group create --no-such-flag myvalue")
	parsed := shell.MatchParams(raw, testParams)
	if len(parsed.Params) != 1 {
		t.Fatalf("Params count = %d, want 1", len(parsed.Params))
	}
	p := parsed.Params[0]
	if !p.Unknown {
		t.Error("Unknown should be true for unrecognised flag")
	}
	if p.Flag != "--no-such-flag" {
		t.Errorf("Flag = %q, want \"--no-such-flag\"", p.Flag)
	}
	if p.Value != "myvalue" {
		t.Errorf("Value = %q, want \"myvalue\"", p.Value)
	}
}

func TestMatchParamsQuotedValue(t *testing.T) {
	raw := rawFor(t, `az group create --name "my group"`)
	parsed := shell.MatchParams(raw, testParams)
	if len(parsed.Params) != 1 {
		t.Fatalf("Params count = %d, want 1", len(parsed.Params))
	}
	if parsed.Params[0].Value != "my group" {
		t.Errorf("Value = %q, want \"my group\" (unquoted)", parsed.Params[0].Value)
	}
	if parsed.Params[0].RawValue != `"my group"` {
		t.Errorf("RawValue = %q, want %q", parsed.Params[0].RawValue, `"my group"`)
	}
}

func TestMatchParamsCursorParam(t *testing.T) {
	line := "az group create --name my-group --location westeurope"
	// "--location" starts at byte 32; cursor at 33 is inside it.
	raw, _ := shell.ParseRaw(line, 33)
	parsed := shell.MatchParams(raw, testParams)
	if parsed.CursorParam != 1 {
		t.Errorf("CursorParam = %d, want 1 (--location at index 1)", parsed.CursorParam)
	}
}

func TestMatchParamsCommand(t *testing.T) {
	raw := rawFor(t, "az group create --name x")
	parsed := shell.MatchParams(raw, testParams)
	if parsed.Command != "group create" {
		t.Errorf("Command = %q, want \"group create\"", parsed.Command)
	}
}

func TestMatchParamsNegativeNumberValue(t *testing.T) {
	params := []metadata.Parameter{
		{Name: "--priority", TakesValue: true, ValueKind: metadata.ValueKindInt},
		{Name: "--name", TakesValue: true, ValueKind: metadata.ValueKindString},
	}
	raw := rawFor(t, "az network nsg rule create --priority -1 --name allow")
	parsed := shell.MatchParams(raw, params)
	if len(parsed.Params) != 2 {
		t.Fatalf("Params count = %d, want 2 (--priority=-1, --name=allow)", len(parsed.Params))
	}
	if parsed.Params[0].Flag != "--priority" || parsed.Params[0].Value != "-1" {
		t.Errorf("Params[0] = {Flag:%q Value:%q}, want {--priority, -1}", parsed.Params[0].Flag, parsed.Params[0].Value)
	}
	if !parsed.Params[0].Explicit {
		t.Error("Params[0].Explicit should be true when a value token was consumed")
	}
	if parsed.Params[1].Flag != "--name" || parsed.Params[1].Value != "allow" {
		t.Errorf("Params[1] = {Flag:%q Value:%q}, want {--name, allow}", parsed.Params[1].Flag, parsed.Params[1].Value)
	}
}

func TestMatchParamsExplicitEmpty(t *testing.T) {
	raw := rawFor(t, "az group create --tags= --name x")
	parsed := shell.MatchParams(raw, testParams)
	if len(parsed.Params) != 2 {
		t.Fatalf("Params count = %d, want 2", len(parsed.Params))
	}
	if parsed.Params[0].Flag != "--tags" {
		t.Errorf("Params[0].Flag = %q, want \"--tags\"", parsed.Params[0].Flag)
	}
	if parsed.Params[0].Value != "" {
		t.Errorf("Params[0].Value = %q, want \"\"", parsed.Params[0].Value)
	}
	if !parsed.Params[0].Explicit {
		t.Error("Params[0].Explicit should be true — `--tags=` provides an explicit empty value")
	}
	if parsed.Params[1].Value != "x" {
		t.Errorf("Params[1].Value = %q, want \"x\" (following flag must not be consumed as value)", parsed.Params[1].Value)
	}
}

func TestMatchParamsMissingValue(t *testing.T) {
	raw := rawFor(t, "az group create --name --location westeurope")
	parsed := shell.MatchParams(raw, testParams)
	if len(parsed.Params) != 2 {
		t.Fatalf("Params count = %d, want 2", len(parsed.Params))
	}
	if parsed.Params[0].Flag != "--name" || parsed.Params[0].Value != "" || parsed.Params[0].Explicit {
		t.Errorf("Params[0] = {Flag:%q Value:%q Explicit:%v}, want {--name, \"\", false}",
			parsed.Params[0].Flag, parsed.Params[0].Value, parsed.Params[0].Explicit)
	}
	if parsed.Params[1].Flag != "--location" || parsed.Params[1].Value != "westeurope" {
		t.Errorf("Params[1] = {Flag:%q Value:%q}, want {--location, westeurope}", parsed.Params[1].Flag, parsed.Params[1].Value)
	}
}

// listParams mirrors az network application-gateway address-pool update:
// --servers takes a space-separated list of IP/DNS names.
var listParams = []metadata.Parameter{
	{Name: "--gateway-name", TakesValue: true, ValueKind: metadata.ValueKindString},
	{Name: "--name", Aliases: []string{"-n"}, TakesValue: true, ValueKind: metadata.ValueKindString},
	{Name: "--resource-group", Aliases: []string{"-g"}, TakesValue: true, ValueKind: metadata.ValueKindString},
	{Name: "--servers", TakesValue: true, ValueKind: metadata.ValueKindList},
	{Name: "--tags", TakesValue: true, ValueKind: metadata.ValueKindKeyValue},
}

func TestMatchParamsListMultiVar(t *testing.T) {
	// The reported bug: --servers "$address1" "$address2" was being parsed
	// as a literal value "$address1 $address2" with IsVar=false, so the
	// rebuild single-quoted it and bash saw one arg instead of two.
	raw := rawFor(t, `az network application-gateway address-pool update --gateway-name X --name Y --resource-group Z --servers "$address1" "$address2"`)
	parsed := shell.MatchParams(raw, listParams)

	servers := parsed.Params[3]
	if servers.Flag != "--servers" {
		t.Fatalf("Params[3].Flag = %q, want --servers", servers.Flag)
	}
	if servers.Value != "$address1 $address2" {
		t.Errorf("Value = %q, want %q (joined)", servers.Value, "$address1 $address2")
	}
	if !servers.IsVar {
		t.Error("IsVar should be true: every consumed token is a var ref")
	}
	if servers.VarName != "" {
		t.Errorf("VarName = %q, want \"\" for multi-var lists", servers.VarName)
	}
	if len(servers.VarNames) != 2 || servers.VarNames[0] != "address1" || servers.VarNames[1] != "address2" {
		t.Errorf("VarNames = %v, want [address1 address2]", servers.VarNames)
	}
}

func TestMatchParamsListSingleVar(t *testing.T) {
	// Single var ref on a list-kind param keeps the old shape: IsVar=true,
	// VarName set, VarNames empty (only one entry to track).
	raw := rawFor(t, `az network application-gateway address-pool update --servers "$a1"`)
	parsed := shell.MatchParams(raw, listParams)

	servers := parsed.Params[0]
	if !servers.IsVar {
		t.Error("IsVar should be true")
	}
	if servers.VarName != "a1" {
		t.Errorf("VarName = %q, want \"a1\"", servers.VarName)
	}
	if len(servers.VarNames) != 0 {
		t.Errorf("VarNames = %v, want empty for single-var list", servers.VarNames)
	}
}

func TestMatchParamsListLiteral(t *testing.T) {
	// Literal list values stay literal — the user explicitly chose to fix
	// only the all-var case; literal lists still go through EscapePOSIX.
	raw := rawFor(t, "az network application-gateway address-pool update --servers 10.0.0.4 10.0.0.5")
	parsed := shell.MatchParams(raw, listParams)

	servers := parsed.Params[0]
	if servers.Value != "10.0.0.4 10.0.0.5" {
		t.Errorf("Value = %q, want %q", servers.Value, "10.0.0.4 10.0.0.5")
	}
	if servers.IsVar {
		t.Error("IsVar should be false for literal list")
	}
	if len(servers.VarNames) != 0 {
		t.Errorf("VarNames = %v, want empty for literal list", servers.VarNames)
	}
}

func TestMatchParamsListMixed(t *testing.T) {
	// Mixed (some var, some literal) is treated as literal: not every
	// consumed token is a var ref, so the safe path is literal mode.
	raw := rawFor(t, `az network application-gateway address-pool update --servers "$addr1" 10.0.0.5`)
	parsed := shell.MatchParams(raw, listParams)

	servers := parsed.Params[0]
	if servers.IsVar {
		t.Error("IsVar should be false for mixed var/literal list")
	}
	if len(servers.VarNames) != 0 {
		t.Errorf("VarNames = %v, want empty for mixed list", servers.VarNames)
	}
}

func TestMatchParamsListStopsAtFlag(t *testing.T) {
	// The list must terminate when the next token starts with '-', not
	// greedily swallow the following flag's value.
	raw := rawFor(t, `az group create --tags env=prod owner=vladi --name x`)
	parsed := shell.MatchParams(raw, testParams)
	if len(parsed.Params) != 2 {
		t.Fatalf("Params count = %d, want 2", len(parsed.Params))
	}
	tags := parsed.Params[0]
	if tags.Flag != "--tags" {
		t.Fatalf("Params[0].Flag = %q, want --tags", tags.Flag)
	}
	if tags.Value != "env=prod owner=vladi" {
		t.Errorf("Value = %q, want %q", tags.Value, "env=prod owner=vladi")
	}
	if parsed.Params[1].Flag != "--name" || parsed.Params[1].Value != "x" {
		t.Errorf("Params[1] = {Flag:%q Value:%q}, want {--name, x}", parsed.Params[1].Flag, parsed.Params[1].Value)
	}
}

// TestMatchParamsPositionalOnlyFlagValues covers the bug where the form
// misclassified every flag value (e.g. myAppGateway, $RG) as a positional
// token. Only true stray words should land in Positional.
func TestMatchParamsPositionalOnlyFlagValues(t *testing.T) {
	// Every non-flag token here is a value for some flag — none are stray.
	raw := rawFor(t, `az network application-gateway address-pool update -g $RG --gateway-name myAppGateway -n appGatewayBackendPool --servers "$addr1" "$addr2"`)
	parsed := shell.MatchParams(raw, listParams)

	if got := len(parsed.Positional); got != 0 {
		names := make([]string, got)
		for i, tok := range parsed.Positional {
			names[i] = tok.Value
		}
		t.Errorf("Positional should be empty when every non-flag token is a flag value; got %v", names)
	}

	// Genuine positional: user forgot --name and just typed a stray word.
	raw = rawFor(t, `az network application-gateway address-pool update -g $RG mystorage`)
	parsed = shell.MatchParams(raw, listParams)
	if len(parsed.Positional) != 1 || parsed.Positional[0].Value != "mystorage" {
		names := make([]string, len(parsed.Positional))
		for i, tok := range parsed.Positional {
			names[i] = tok.Value
		}
		t.Errorf("Positional should contain stray word; got %v", names)
	}
}

func TestMatchParamsListBracedVars(t *testing.T) {
	// ${NAME} form must also be recognised per token.
	raw := rawFor(t, `az network application-gateway address-pool update --servers "${addr1}" "${addr2}"`)
	parsed := shell.MatchParams(raw, listParams)

	servers := parsed.Params[0]
	if !servers.IsVar {
		t.Error("IsVar should be true for braced var refs")
	}
	if len(servers.VarNames) != 2 || servers.VarNames[0] != "addr1" || servers.VarNames[1] != "addr2" {
		t.Errorf("VarNames = %v, want [addr1 addr2]", servers.VarNames)
	}
}

func TestMatchParamsSingleQuotedListVars(t *testing.T) {
	// Single-quoted $addr1 — the existing detectVarRef treats it as a var
	// ref by string shape (it doesn't inspect the original quoting). That
	// pre-existing behaviour is preserved here; this fix only adds support
	// for the multi-token list case, not for reclassifying single-quoted
	// refs. If a future change wants to honour single-quotes as literal it
	// must update detectVarRef and the single-token tests too.
	raw := rawFor(t, `az network application-gateway address-pool update --servers '$addr1' '$addr2'`)
	parsed := shell.MatchParams(raw, listParams)

	servers := parsed.Params[0]
	if !servers.IsVar {
		t.Error("IsVar should be true (matches existing single-token behaviour)")
	}
	if len(servers.VarNames) != 2 || servers.VarNames[0] != "addr1" || servers.VarNames[1] != "addr2" {
		t.Errorf("VarNames = %v, want [addr1 addr2]", servers.VarNames)
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "a", 1},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"resource-group", "resource-groop", 1},
		{"--name", "--nane", 1},
	}
	for _, tc := range cases {
		got := shell.Levenshtein(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("Levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// Round-trip: parsing a line and rebuilding it via MatchParams must produce
// the same command semantics (same canonical flags and unquoted values).
func TestRoundTrip(t *testing.T) {
	params := []metadata.Parameter{
		{Name: "--name", Aliases: []string{"-n"}, TakesValue: true, ValueKind: metadata.ValueKindString},
		{Name: "--location", Aliases: []string{"-l"}, TakesValue: true, ValueKind: metadata.ValueKindString},
		{Name: "--resource-group", Aliases: []string{"-g"}, TakesValue: true, ValueKind: metadata.ValueKindString},
	}
	cases := []struct {
		line       string
		wantFlags  []string
		wantValues []string
	}{
		{
			"az group create --name my-group --location westeurope",
			[]string{"--name", "--location"},
			[]string{"my-group", "westeurope"},
		},
		{
			`az group create --name "my group" --location westeurope`,
			[]string{"--name", "--location"},
			[]string{"my group", "westeurope"},
		},
		{
			"az group create -n my-group -l westeurope",
			[]string{"--name", "--location"},
			[]string{"my-group", "westeurope"},
		},
		{
			"az group create --resource-group $RG --location westeurope",
			[]string{"--resource-group", "--location"},
			[]string{"$RG", "westeurope"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			raw, ok := shell.ParseRaw(tc.line, 0)
			if !ok {
				t.Fatal("ParseRaw failed")
			}
			parsed := shell.MatchParams(raw, params)
			if len(parsed.Params) != len(tc.wantFlags) {
				t.Fatalf("Params count = %d, want %d", len(parsed.Params), len(tc.wantFlags))
			}
			for i, wf := range tc.wantFlags {
				if parsed.Params[i].Flag != wf {
					t.Errorf("Params[%d].Flag = %q, want %q", i, parsed.Params[i].Flag, wf)
				}
				if parsed.Params[i].Value != tc.wantValues[i] {
					t.Errorf("Params[%d].Value = %q, want %q", i, parsed.Params[i].Value, tc.wantValues[i])
				}
			}
		})
	}
}
