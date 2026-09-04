// Package validate implements the offline validation engine described in spec
// §9. Rules live behind the Rule interface; the form depends on this package
// but never the reverse (spec §9.1).
package validate

import (
	"context"

	"github.com/someson/azform/internal/metadata"
)

// FieldMode mirrors the UI's FieldMode so rules can inspect var/literal mode
// without importing the UI package. Keep the iota values identical.
type FieldMode int

const (
	FieldModeLiteral FieldMode = iota
	FieldModeVar
)

// Severity classifies a finding's impact on form submission.
type Severity int

const (
	SeverityWarning Severity = iota
	SeverityBlocking
)

// Finding is a single violation surfaced to the user. Param is the canonical
// flag name (or "" for command-level findings). Message is rendered in the
// footer one-liner; Suggest is an optional value the user can accept with one
// key. RuleID is stable so it can be filtered in config and searched in logs.
type Finding struct {
	Param    string
	Severity Severity
	Message  string
	Suggest  string
	RuleID   string
}

// Rule is one validation check.
type Rule interface {
	ID() string
	Check(cmd *metadata.Command, state *FormState) []Finding
}

// RuleProvider yields rules for a given command. The engine accepts multiple
// providers and concatenates their findings (spec §9.1).
type RuleProvider interface {
	Rules(ctx context.Context, command string) ([]Rule, error)
}

// FormState is the snapshot of the form that rules inspect. The form builds
// this on every validation pass; rules are pure and must not mutate it.
type FormState struct {
	Params          []metadata.Parameter
	Values          map[string]string
	Modes           map[string]FieldMode
	Enabled         map[string]bool
	UnknownFlags    []string
	Positional      []string
	SessionVars     map[string]bool
	UsedVarNames    []string
	FlagOccurrences map[string]int
}
