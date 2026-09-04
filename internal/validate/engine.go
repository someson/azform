package validate

import (
	"context"

	"github.com/someson/azform/internal/metadata"
)

// Engine runs a fixed list of providers and concatenates their findings.
type Engine struct {
	providers []RuleProvider
}

// NewEngine constructs an Engine from one or more providers. Provider order
// determines the order of findings in the result.
func NewEngine(providers ...RuleProvider) *Engine {
	return &Engine{providers: append([]RuleProvider(nil), providers...)}
}

// Run evaluates every rule from every provider against state. Provider errors
// are silently skipped — a partial result is better than blocking the form
// because one rule source is unavailable.
func (e *Engine) Run(ctx context.Context, cmd *metadata.Command, state *FormState) []Finding {
	var out []Finding
	for _, p := range e.providers {
		rules, err := p.Rules(ctx, commandOf(cmd))
		if err != nil {
			continue
		}
		for _, r := range rules {
			out = append(out, r.Check(cmd, state)...)
		}
	}
	return out
}

func commandOf(cmd *metadata.Command) string {
	if cmd == nil {
		return ""
	}
	return cmd.Command
}
