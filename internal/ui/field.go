package ui

import (
	"time"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/validate"
)

// FieldMode mirrors validate.FieldMode so the form can pass it through.
// Spec §9.1: the validate package owns the type, ui re-exports.
type FieldMode = validate.FieldMode

const (
	FieldModeLiteral = validate.FieldModeLiteral
	FieldModeVar     = validate.FieldModeVar
)

// FieldSource records where the field's current value came from, for display.
type FieldSource int

const (
	FieldSourceNone       FieldSource = iota
	FieldSourceDefault                // from metadata Default field
	FieldSourceDraft                  // restored from draft (spec 6.7)
	FieldSourceEnv                    // from shell variables (M4, spec 4.2)
	FieldSourceBuffer                 // from parsed shell buffer (spec 6.8)
	FieldSourceAzure                  // from ~/.azure/config / AZURE_DEFAULTS_* (M4, spec 4.4)
	FieldSourceRemembered             // from bindings.json (M6, spec 8.1/8.2)
)

// FetchState tracks the lazy ARM/az fetch lifecycle for a field (spec §6.1
// Field fetch state). Only fields with a non-empty ValuesFrom hint ever
// leave FetchIdle.
type FetchState int

const (
	FetchIdle FetchState = iota
	FetchLoading
	FetchLoaded
	FetchError
)

// String returns the lowercase name used for debug logging and tests.
func (s FetchState) String() string {
	switch s {
	case FetchLoading:
		return "loading"
	case FetchLoaded:
		return "loaded"
	case FetchError:
		return "error"
	default:
		return "idle"
	}
}

// Field pairs one metadata.Parameter with its current form state.
type Field struct {
	Param    metadata.Parameter
	Value    string
	VarValue string // resolved value for display when Source is Env/Buffer: "$RG" displays as "$RG → my-group"
	Mode     FieldMode
	Enabled  bool // whether this param is included in the output command
	Source   FieldSource

	// Lazy fetch state (spec §6.1). Idle for fields with no ValuesFrom; the
	// cursor-move logic in model.go promotes Idle → Loading on focus.
	FetchState       FetchState
	FetchedChoices   []string  // populated when FetchState == FetchLoaded
	FetchError       string    // populated when FetchState == FetchError
	FetchStartedAt   time.Time // when Loading began; used by the 3 s / 10 s ticks
	FetchSpinnerShow bool      // gated by the 150 ms visibility tick; suppresses flicker on fast loads
}

// ToRenderValue returns the value and whether it is a var ref (for render.Build).
func (f *Field) ToRenderValue() (value string, isVar bool) {
	return f.Value, f.Mode == FieldModeVar
}

// DisplayValue returns the value to show in the form list. For var-mode fields
// it returns "$VAR → resolved" so the user can see both the reference and the
// value the shell will substitute.
func (f *Field) DisplayValue() string {
	if f.Mode == FieldModeVar && f.VarValue != "" {
		return f.Value + " → " + f.VarValue
	}
	return f.Value
}

// SourceTag returns the short parenthetical label shown beside the value.
func (f *Field) SourceTag() string {
	switch f.Source {
	case FieldSourceDraft:
		return "(draft)"
	case FieldSourceEnv:
		return "(env)"
	case FieldSourceBuffer:
		return "(buf)"
	case FieldSourceAzure:
		return "(azure)"
	case FieldSourceRemembered:
		return "(remembered)"
	default:
		return ""
	}
}

// Name returns the stable lowercase name used for debug logs. The set is
// closed: every FieldSource constant maps to a known string. NEVER log the
// field's Value here — debug output must contain only param names and
// source identifiers (spec §15.3, last paragraph).
func (s FieldSource) Name() string {
	switch s {
	case FieldSourceDefault:
		return "default"
	case FieldSourceDraft:
		return "draft"
	case FieldSourceEnv:
		return "env"
	case FieldSourceBuffer:
		return "buffer"
	case FieldSourceAzure:
		return "azure"
	case FieldSourceRemembered:
		return "remembered"
	default:
		return "none"
	}
}

// ModeName returns the lowercase name used for debug logs ("literal"/"var").
func ModeName(m FieldMode) string {
	if m == FieldModeVar {
		return "var"
	}
	return "literal"
}
