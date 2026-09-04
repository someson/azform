package metadata

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGoldens = flag.Bool("update", false, "update parser golden files")

func fixturePath(name string) string {
	return filepath.Join("..", "..", "testdata", "help", name)
}

func goldenPath(name string) string {
	name = strings.TrimSuffix(name, filepath.Ext(name)) + ".json"
	return filepath.Join("..", "..", "testdata", "golden", name)
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(fixturePath(name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(data)
}

func parseFixture(t *testing.T, name string) *Document {
	t.Helper()
	doc, err := Parse(readFixture(t, name))
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	if err := ValidateDocument(doc); err != nil {
		t.Fatalf("ValidateDocument(%s): %v", name, err)
	}
	return doc
}

func TestParseFixturesGolden(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "testdata", "help"))
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	fixtures := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") || entry.Name() == "az-version.txt" {
			continue
		}
		fixtures++
		t.Run(entry.Name(), func(t *testing.T) {
			doc := parseFixture(t, entry.Name())
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			if err := enc.Encode(doc); err != nil {
				t.Fatalf("encode golden candidate: %v", err)
			}

			path := goldenPath(entry.Name())
			if *updateGoldens {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("create golden dir: %v", err)
				}
				if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
					t.Fatalf("update golden: %v", err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run go test ./internal/metadata -update): %v", err)
			}
			if !bytes.Equal(buf.Bytes(), want) {
				t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", entry.Name(), buf.Bytes(), want)
			}
		})
	}
	if fixtures < 40 {
		t.Fatalf("fixture corpus too small: got %d, want at least 40", fixtures)
	}
}

func TestFixtureHealth(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "testdata", "help"))
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") || entry.Name() == "az-version.txt" {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			doc := parseFixture(t, entry.Name())
			if doc.ParseHealth.UnparsedLines != 0 {
				t.Fatalf("unparsed lines: got %d", doc.ParseHealth.UnparsedLines)
			}
			if !doc.ParseHealth.SectionsOK {
				t.Fatalf("sections not OK")
			}
			if doc.ParseHealth.Params == 0 {
				t.Fatalf("no parameters or navigation entries parsed")
			}
		})
	}
}

func TestGroupCreateMetadata(t *testing.T) {
	doc := parseFixture(t, "group-create.txt")
	cmd := doc.Command
	if doc.Kind != DocumentKindCommand || cmd == nil {
		t.Fatalf("kind=%q command=%v", doc.Kind, cmd)
	}
	if cmd.Command != "group create" {
		t.Fatalf("command: got %q", cmd.Command)
	}
	if cmd.Summary != "Create a new resource group." {
		t.Fatalf("summary: got %q", cmd.Summary)
	}

	location := cmd.FindParameter("--location")
	if location == nil {
		t.Fatalf("--location not parsed")
	}
	if !location.Required || location.Group != "Required Parameters" || location.Global {
		t.Fatalf("unexpected location metadata: %+v", location)
	}
	if location.ValuesFrom == nil || *location.ValuesFrom != "az account list-locations" {
		t.Fatalf("location values_from: %v", location.ValuesFrom)
	}

	name := cmd.FindParameter("--name")
	if name == nil {
		t.Fatalf("--name not parsed")
	}
	wantAliases := []string{"--resource-group", "-g", "-n"}
	if strings.Join(name.Aliases, ",") != strings.Join(wantAliases, ",") {
		t.Fatalf("name aliases: got %v, want %v", name.Aliases, wantAliases)
	}
	if cmd.FindParameter("-g") != name {
		t.Fatalf("alias lookup did not return canonical --name parameter")
	}

	tags := cmd.FindParameter("--tags")
	if tags == nil || tags.ValueKind != ValueKindKeyValue {
		t.Fatalf("tags metadata: %+v", tags)
	}

	output := cmd.FindParameter("--output")
	if output == nil || !output.Global || output.ValueKind != ValueKindEnum {
		t.Fatalf("global --output metadata: %+v", output)
	}
	if output.Default == nil || *output.Default != "json" {
		t.Fatalf("global --output default: %v", output.Default)
	}
	if !containsString(output.Choices, "jsonc") || !containsString(output.Choices, "yamlc") {
		t.Fatalf("global --output choices: %v", output.Choices)
	}
}

func TestStorageAccountCreateChoicesAndDefaults(t *testing.T) {
	cmd := parseFixture(t, "storage-account-create.txt").Command

	accessTier := cmd.FindParameter("--access-tier")
	if accessTier == nil || accessTier.ValueKind != ValueKindEnum {
		t.Fatalf("access-tier metadata: %+v", accessTier)
	}
	for _, choice := range []string{"Cold", "Cool", "Hot", "Premium", "Smart"} {
		if !containsString(accessTier.Choices, choice) {
			t.Fatalf("access-tier choices missing %q: %v", choice, accessTier.Choices)
		}
	}
	if strings.Contains(accessTier.Help, "Allowed values:") {
		t.Fatalf("structured marker left in help: %q", accessTier.Help)
	}

	publicAccess := cmd.FindParameter("--allow-blob-public-access")
	if publicAccess == nil || publicAccess.ValueKind != ValueKindBool || !publicAccess.TakesValue {
		t.Fatalf("bool metadata: %+v", publicAccess)
	}

	sku := cmd.FindParameter("--sku")
	if sku == nil || sku.ValueKind != ValueKindEnum {
		t.Fatalf("sku metadata: %+v", sku)
	}
	if sku.Default == nil || *sku.Default != "Standard_RAGRS" {
		t.Fatalf("sku default: %v", sku.Default)
	}
	for _, choice := range []string{"Premium_LRS", "Standard_GRS", "Standard_LRS", "Standard_RAGRS"} {
		if !containsString(sku.Choices, choice) {
			t.Fatalf("sku choices missing %q: %v", choice, sku.Choices)
		}
	}

	tls := cmd.FindParameter("--min-tls-version")
	if tls == nil || tls.ValueKind != ValueKindEnum {
		t.Fatalf("min-tls metadata: %+v", tls)
	}
	for _, choice := range []string{"TLS1_0", "TLS1_1", "TLS1_2", "TLS1_3"} {
		if !containsString(tls.Choices, choice) {
			t.Fatalf("min-tls choices missing %q: %v", choice, tls.Choices)
		}
	}
}

func TestVMCreateArgumentGroups(t *testing.T) {
	cmd := parseFixture(t, "vm-create.txt").Command
	if cmd.Command != "vm create" {
		t.Fatalf("command: %q", cmd.Command)
	}
	if !cmd.FindParameter("--name").Required || !cmd.FindParameter("--resource-group").Required {
		t.Fatalf("required parameters not extracted")
	}
	adminUsername := cmd.FindParameter("--admin-username")
	if adminUsername == nil || adminUsername.Group != "Authentication Arguments" {
		t.Fatalf("admin username group: %+v", adminUsername)
	}
	image := cmd.FindParameter("--image")
	if image == nil || image.ValueKind != ValueKindString || len(image.Choices) != 0 {
		t.Fatalf("image metadata: %+v", image)
	}
	noWait := cmd.FindParameter("--no-wait")
	if noWait == nil || noWait.ValueKind != ValueKindBool || noWait.TakesValue {
		t.Fatalf("no-wait switch metadata: %+v", noWait)
	}
}

func TestGroupNavigation(t *testing.T) {
	doc := parseFixture(t, "storage-account.txt")
	group := doc.Group
	if doc.Kind != DocumentKindGroup || group == nil {
		t.Fatalf("kind=%q group=%v", doc.Kind, group)
	}
	if group.Group != "storage account" || group.Summary != "Manage storage accounts." {
		t.Fatalf("group identity: %+v", group)
	}
	if len(group.Subgroups) == 0 || len(group.Commands) == 0 {
		t.Fatalf("navigation is empty: %+v", group)
	}

	var sawPreviewSubgroup, sawCreate bool
	for _, item := range group.Subgroups {
		if item.Name == "blob-inventory-policy" && item.Preview {
			sawPreviewSubgroup = true
		}
	}
	for _, item := range group.Commands {
		if item.Name == "create" {
			sawCreate = true
		}
	}
	if !sawPreviewSubgroup || !sawCreate {
		t.Fatalf("navigation did not preserve expected entries: %+v", group)
	}
}

func TestExtensionAndPositionalFixtures(t *testing.T) {
	bastion := parseFixture(t, "network-bastion-create.txt").Command
	if bastion.Command != "network bastion create" {
		t.Fatalf("extension command path: %q", bastion.Command)
	}
	if !bastion.FindParameter("--vnet-name").Required {
		t.Fatalf("extension required parameter not extracted")
	}

	ssh := parseFixture(t, "network-bastion-ssh.txt").Command
	positional := ssh.FindParameter("<SSH_ARGS>")
	if positional == nil || positional.Group != "Positional" || positional.ValueKind != ValueKindList {
		t.Fatalf("positional metadata: %+v", positional)
	}
}

func TestNumericChoicesAndPathSyntax(t *testing.T) {
	redis := parseFixture(t, "redis-create.txt").Command
	tls := redis.FindParameter("--minimum-tls-version")
	if tls == nil || tls.ValueKind != ValueKindEnum {
		t.Fatalf("redis tls metadata: %+v", tls)
	}
	for _, choice := range []string{"1.0", "1.1", "1.2"} {
		if !containsString(tls.Choices, choice) {
			t.Fatalf("numeric choices were split incorrectly: %v", tls.Choices)
		}
	}

	container := parseFixture(t, "container-create.txt").Command
	gitRepoDir := container.FindParameter("--gitrepo-dir")
	if gitRepoDir == nil || gitRepoDir.Default == nil || *gitRepoDir.Default != "." {
		t.Fatalf("single-dot default was not preserved: %+v", gitRepoDir)
	}

	deployment := parseFixture(t, "deployment-group-create.txt").Command
	parameters := deployment.FindParameter("--parameters")
	if parameters == nil || parameters.ValueKind != ValueKindPath {
		t.Fatalf("--parameters should accept @file and be path-capable: %+v", parameters)
	}
}

func TestParseRejectsUnknownPage(t *testing.T) {
	if _, err := Parse("not an az help page\n"); err == nil {
		t.Fatalf("Parse accepted a page without Command/Group header")
	}
}
