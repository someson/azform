package validate_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/someson/azform/internal/metadata"
	"github.com/someson/azform/internal/validate"
)

type fakeProvider struct{ rules []validate.Rule }

func (f *fakeProvider) Rules(ctx context.Context, command string) ([]validate.Rule, error) {
	return f.rules, nil
}

type errorProvider struct{}

func (errorProvider) Rules(ctx context.Context, command string) ([]validate.Rule, error) {
	return nil, errors.New("kaboom")
}

type alwaysRule struct {
	id       string
	severity validate.Severity
	msg      string
	param    string
}

func (r *alwaysRule) ID() string { return r.id }
func (r *alwaysRule) Check(cmd *metadata.Command, st *validate.FormState) []validate.Finding {
	return []validate.Finding{{
		Param:    r.param,
		Severity: r.severity,
		Message:  r.msg,
		RuleID:   r.id,
	}}
}

func TestEngineAggregatesFindings(t *testing.T) {
	rules := []validate.Rule{
		&alwaysRule{id: "test/a", severity: validate.SeverityWarning, msg: "first", param: ""},
		&alwaysRule{id: "test/b", severity: validate.SeverityBlocking, msg: "second", param: "--name"},
	}
	eng := validate.NewEngine(&fakeProvider{rules: rules})
	got := eng.Run(context.Background(), &metadata.Command{Command: "x"}, &validate.FormState{})
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2", len(got))
	}
	if got[0].RuleID != "test/a" || got[1].RuleID != "test/b" {
		t.Errorf("findings order not preserved: %+v", got)
	}
}

func TestEngineCombinesMultipleProviders(t *testing.T) {
	p1 := &fakeProvider{rules: []validate.Rule{&alwaysRule{id: "p1/r", msg: "one"}}}
	p2 := &fakeProvider{rules: []validate.Rule{&alwaysRule{id: "p2/r", msg: "two"}}}
	eng := validate.NewEngine(p1, p2)
	got := eng.Run(context.Background(), &metadata.Command{}, &validate.FormState{})
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2 (one per provider)", len(got))
	}
	ids := []string{got[0].RuleID, got[1].RuleID}
	want := []string{"p1/r", "p2/r"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("got order %v, want %v", ids, want)
	}
}

func TestEngineTolerantOfProviderErrors(t *testing.T) {
	rules := []validate.Rule{&alwaysRule{id: "p1/r", msg: "one"}}
	eng := validate.NewEngine(errorProvider{}, &fakeProvider{rules: rules})
	got := eng.Run(context.Background(), &metadata.Command{}, &validate.FormState{})
	if len(got) != 1 || got[0].RuleID != "p1/r" {
		t.Errorf("got %+v, want 1 finding from the good provider", got)
	}
}
