package eval

import (
	"testing"

	"github.com/pia/porthole/internal/model"
)

func TestMatches(t *testing.T) {
	labels := map[string]string{"app": "web", "tier": "2"}
	cases := []struct {
		name string
		sel  model.LabelSelector
		want bool
	}{
		{"empty matches all", model.LabelSelector{}, true},
		{"label hit", model.LabelSelector{MatchLabels: map[string]string{"app": "web"}}, true},
		{"label miss", model.LabelSelector{MatchLabels: map[string]string{"app": "db"}}, false},
		{"absent key", model.LabelSelector{MatchLabels: map[string]string{"env": "prod"}}, false},
		{"in", model.LabelSelector{MatchExpressions: []model.LabelExpression{{Key: "app", Operator: "In", Values: []string{"db", "web"}}}}, true},
		{"in absent", model.LabelSelector{MatchExpressions: []model.LabelExpression{{Key: "env", Operator: "In", Values: []string{"prod"}}}}, false},
		{"notin value", model.LabelSelector{MatchExpressions: []model.LabelExpression{{Key: "app", Operator: "NotIn", Values: []string{"db"}}}}, true},
		{"notin same", model.LabelSelector{MatchExpressions: []model.LabelExpression{{Key: "app", Operator: "NotIn", Values: []string{"web"}}}}, false},
		{"notin absent key", model.LabelSelector{MatchExpressions: []model.LabelExpression{{Key: "env", Operator: "NotIn", Values: []string{"prod"}}}}, true},
		{"exists", model.LabelSelector{MatchExpressions: []model.LabelExpression{{Key: "app", Operator: "Exists"}}}, true},
		{"exists miss", model.LabelSelector{MatchExpressions: []model.LabelExpression{{Key: "env", Operator: "Exists"}}}, false},
		{"does not exist", model.LabelSelector{MatchExpressions: []model.LabelExpression{{Key: "env", Operator: "DoesNotExist"}}}, true},
		{"gt", model.LabelSelector{MatchExpressions: []model.LabelExpression{{Key: "tier", Operator: "Gt", Values: []string{"1"}}}}, true},
		{"lt", model.LabelSelector{MatchExpressions: []model.LabelExpression{{Key: "tier", Operator: "Lt", Values: []string{"2"}}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Matches(tc.sel, labels); got != tc.want {
				t.Fatalf("Matches() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIPInBlock(t *testing.T) {
	block := model.IPBlock{CIDR: "10.0.0.0/8", Except: []string{"10.1.0.0/24"}}
	if !IPInBlock("10.2.0.1", block) {
		t.Fatal("expected 10.2.0.1 inside 10.0.0.0/8")
	}
	if IPInBlock("10.1.0.5", block) {
		t.Fatal("expected 10.1.0.5 excluded")
	}
	if IPInBlock("11.0.0.1", block) {
		t.Fatal("expected 11.0.0.1 outside")
	}
	if _, ok := SampleIP(block); !ok {
		t.Fatal("expected a sample IP")
	}
}
