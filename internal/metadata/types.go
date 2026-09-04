// Package metadata extracts Azure CLI command metadata from `az --help`
// output and stores it in the lazy on-disk cache described by the azform
// specification.
package metadata

import "time"

const (
	// SchemaVersion versions the JSON cache record format. It does not version
	// parser quality; cache entries also carry AzformVersion for that reason.
	SchemaVersion = 1

	// SourceHelpParser identifies metadata produced by parsing az --help.
	SourceHelpParser = "help-parser"
)

// ValueKind is the editing/rendering category used later by the form.
type ValueKind string

const (
	ValueKindString   ValueKind = "string"
	ValueKindEnum     ValueKind = "enum"
	ValueKindBool     ValueKind = "bool"
	ValueKindInt      ValueKind = "int"
	ValueKindList     ValueKind = "list"
	ValueKindKeyValue ValueKind = "keyvalue"
	ValueKindPath     ValueKind = "path"
)

// Parameter is one command-line argument in the cache schema from spec 3.2.
type Parameter struct {
	Name       string    `json:"name"`
	Aliases    []string  `json:"aliases"`
	Required   bool      `json:"required"`
	Group      string    `json:"group"`
	Global     bool      `json:"global"`
	TakesValue bool      `json:"takes_value"`
	ValueKind  ValueKind `json:"value_kind"`
	Help       string    `json:"help"`
	Choices    []string  `json:"choices"`
	Default    *string   `json:"default"`
	ValuesFrom *string   `json:"values_from"`
}

// AllNames returns the canonical name followed by aliases.
func (p Parameter) AllNames() []string {
	out := make([]string, 0, 1+len(p.Aliases))
	out = append(out, p.Name)
	out = append(out, p.Aliases...)
	return out
}

// Command is the deterministic result of parsing a command help page.
type Command struct {
	Command    string      `json:"command"`
	Summary    string      `json:"summary"`
	Parameters []Parameter `json:"parameters"`
}

// FindParameter returns a parameter by canonical name or alias.
func (c *Command) FindParameter(name string) *Parameter {
	if c == nil {
		return nil
	}
	for i := range c.Parameters {
		p := &c.Parameters[i]
		if p.Name == name {
			return p
		}
		for _, alias := range p.Aliases {
			if alias == name {
				return p
			}
		}
	}
	return nil
}

// IsSwitch reports whether the parameter is a bare bool switch — a flag that
// doesn't accept a value. True when takes_value=false and value_kind=bool with
// no explicit choices list (which is how az documents switches like --debug,
// --help, --verbose). Used by the form to omit the value column and by the
// validator to skip enum checks.
func (p Parameter) IsSwitch() bool {
	return !p.TakesValue && p.ValueKind == ValueKindBool && len(p.Choices) == 0
}

// NavigationItem is one entry on an Azure CLI group help page.
type NavigationItem struct {
	Name         string `json:"name"`
	Summary      string `json:"summary"`
	Preview      bool   `json:"preview,omitempty"`
	Deprecated   bool   `json:"deprecated,omitempty"`
	Experimental bool   `json:"experimental,omitempty"`
}

// Group is the deterministic result of parsing a group help page.
type Group struct {
	Group     string           `json:"group"`
	Summary   string           `json:"summary"`
	Subgroups []NavigationItem `json:"subgroups"`
	Commands  []NavigationItem `json:"commands"`
}

// ParseHealth contains the self-diagnostics written to every cache record.
type ParseHealth struct {
	Params        int  `json:"params"`
	UnparsedLines int  `json:"unparsed_lines"`
	SectionsOK    bool `json:"sections_ok"`
}

// Suspect reports whether the parse shows signs of format degradation.
func (h ParseHealth) Suspect() bool {
	return h.Params == 0 || h.UnparsedLines > 0 || !h.SectionsOK
}

// DocumentKind identifies the two kinds of az help pages azform consumes.
type DocumentKind string

const (
	DocumentKindCommand DocumentKind = "command"
	DocumentKindGroup   DocumentKind = "group"
)

// Document is the deterministic parser output. Cache records add runtime
// fields (versions, source, timestamps) around this value.
type Document struct {
	Kind        DocumentKind `json:"kind"`
	ParseHealth ParseHealth  `json:"parse_health"`
	Command     *Command     `json:"command,omitempty"`
	Group       *Group       `json:"group,omitempty"`
}

// CommandRecord is the JSON schema stored under commands/<slug>.json.
type CommandRecord struct {
	SchemaVersion int         `json:"schema_version"`
	Command       string      `json:"command"`
	Summary       string      `json:"summary"`
	AZVersion     string      `json:"az_version"`
	AzformVersion string      `json:"azform_version"`
	Source        string      `json:"source"`
	GeneratedAt   time.Time   `json:"generated_at"`
	ParseHealth   ParseHealth `json:"parse_health"`
	Parameters    []Parameter `json:"parameters"`
}

// GroupRecord is the JSON schema stored under groups/<slug>.json.
type GroupRecord struct {
	SchemaVersion int              `json:"schema_version"`
	Group         string           `json:"group"`
	Summary       string           `json:"summary"`
	AZVersion     string           `json:"az_version"`
	AzformVersion string           `json:"azform_version"`
	Source        string           `json:"source"`
	GeneratedAt   time.Time        `json:"generated_at"`
	ParseHealth   ParseHealth      `json:"parse_health"`
	Subgroups     []NavigationItem `json:"subgroups"`
	Commands      []NavigationItem `json:"commands"`
}
