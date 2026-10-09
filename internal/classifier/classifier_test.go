package classifier

import "testing"

// TestNewClassifierSkipsInvalidRulePattern ensures a malformed rule regex does
// not abort startup: the classifier is still created and the bad rule is simply
// omitted while valid rules remain.
func TestNewClassifierSkipsInvalidRulePattern(t *testing.T) {
	clf, err := NewClassifier(Config{
		Rules: []RuleConfig{
			// Invalid: `\∫` is an invalid escape sequence for Go's regexp.
			{Name: "bad", Pattern: `[\∫]`, Model: "x", Priority: 10},
			{Name: "good", Pattern: `(?i)code`, Model: "y", Priority: 5},
		},
	})
	if err != nil {
		t.Fatalf("NewClassifier should not fail on a bad rule pattern: %v", err)
	}
	if clf == nil {
		t.Fatal("expected a classifier, got nil")
	}
	if len(clf.rules) != 1 {
		t.Fatalf("expected 1 valid rule, got %d", len(clf.rules))
	}
	if clf.rules[0].Name != "good" {
		t.Errorf("expected surviving rule %q, got %q", "good", clf.rules[0].Name)
	}
}

// TestNewClassifierSkipsInvalidCapabilityPattern ensures a malformed capability
// pattern is skipped without aborting startup.
func TestNewClassifierSkipsInvalidCapabilityPattern(t *testing.T) {
	clf, err := NewClassifier(Config{
		Capabilities: map[string]CapabilityConfig{
			"math": {Patterns: []string{`[\∫]`, `\d+`}},
		},
	})
	if err != nil {
		t.Fatalf("NewClassifier should not fail on a bad capability pattern: %v", err)
	}
	cap, ok := clf.capabilities["math"]
	if !ok {
		t.Fatal("expected math capability to exist")
	}
	if len(cap.Patterns) != 1 {
		t.Fatalf("expected 1 valid pattern, got %d", len(cap.Patterns))
	}
}
