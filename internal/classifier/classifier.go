// Package classifier provides request classification for model routing.
package classifier

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/rs/zerolog/log"
)

// TaskCategory represents a type of task.
type TaskCategory string

const (
	// TaskCode is for programming and code-related tasks.
	TaskCode TaskCategory = "code"
	
	// TaskReasoning is for complex reasoning and analysis tasks.
	TaskReasoning TaskCategory = "reasoning"
	
	// TaskSimple is for simple, quick questions.
	TaskSimple TaskCategory = "simple"
	
	// TaskCreative is for creative writing tasks.
	TaskCreative TaskCategory = "creative"
	
	// TaskMath is for mathematical tasks.
	TaskMath TaskCategory = "math"
	
	// TaskGeneral is the default category.
	TaskGeneral TaskCategory = "general"
)

// ClassificationResult contains the result of classifying a request.
type ClassificationResult struct {
	// Category is the detected task category.
	Category TaskCategory
	
	// Confidence is the confidence level (0.0 to 1.0).
	Confidence float64
	
	// Signals contains the signals that led to this classification.
	Signals []string
	
	// InputLength is the character count of the input.
	InputLength int
	
	// TokenEstimate is the estimated token count.
	TokenEstimate int
}

// Classifier classifies requests into task categories.
type Classifier struct {
	capabilities map[TaskCategory]*Capability
	rules        []*Rule
}

// Capability defines how to detect a task category.
type Capability struct {
	Keywords       []string
	Patterns       []*regexp.Regexp
	MaxInputTokens int
}

// Rule is a custom routing rule.
type Rule struct {
	Name     string
	Pattern  *regexp.Regexp
	Model    string
	Priority int
}

// Config contains classifier configuration.
type Config struct {
	Capabilities map[string]CapabilityConfig
	Rules        []RuleConfig
}

// CapabilityConfig is the config for a capability.
type CapabilityConfig struct {
	Keywords       []string
	Patterns       []string
	MaxInputTokens int
}

// RuleConfig is the config for a rule.
type RuleConfig struct {
	Name     string
	Pattern  string
	Model    string
	Priority int
}

// NewClassifier creates a new classifier with the given configuration.
func NewClassifier(config Config) (*Classifier, error) {
	c := &Classifier{
		capabilities: make(map[TaskCategory]*Capability),
	}
	
	// Build capabilities
	for name, cfg := range config.Capabilities {
		cap := &Capability{
			Keywords:       cfg.Keywords,
			MaxInputTokens: cfg.MaxInputTokens,
		}
		
		// Compile patterns
		for _, p := range cfg.Patterns {
			re, err := regexp.Compile(p)
			if err != nil {
				// A malformed capability pattern must not abort startup;
				// log it and skip so the rest of the config still loads.
				log.Warn().
					Str("capability", name).
					Str("pattern", p).
					Err(err).
					Msg("Skipping invalid capability pattern")
				continue
			}
			cap.Patterns = append(cap.Patterns, re)
		}
		
		c.capabilities[TaskCategory(name)] = cap
	}
	
	// Build rules
	for _, cfg := range config.Rules {
		re, err := regexp.Compile(cfg.Pattern)
		if err != nil {
			// A malformed rule pattern must not abort startup; log it and
			// skip so a single bad rule can't take the whole router down.
			log.Warn().
				Str("rule", cfg.Name).
				Str("pattern", cfg.Pattern).
				Err(err).
				Msg("Skipping invalid rule pattern")
			continue
		}
		c.rules = append(c.rules, &Rule{
			Name:     cfg.Name,
			Pattern:  re,
			Model:    cfg.Model,
			Priority: cfg.Priority,
		})
	}
	
	// Sort rules by priority (highest first)
	sortRules(c.rules)
	
	return c, nil
}

// sortRules sorts rules by priority in descending order.
func sortRules(rules []*Rule) {
	for i := 0; i < len(rules); i++ {
		for j := i + 1; j < len(rules); j++ {
			if rules[j].Priority > rules[i].Priority {
				rules[i], rules[j] = rules[j], rules[i]
			}
		}
	}
}

// Classify classifies the given text into a task category.
func (c *Classifier) Classify(text string) *ClassificationResult {
	result := &ClassificationResult{
		Category:      TaskGeneral,
		Confidence:    0.5,
		InputLength:   len(text),
		TokenEstimate: estimateTokens(text),
	}
	
	// Normalize text for comparison
	normalizedText := strings.ToLower(text)
	
	// Check custom rules first
	if model := c.matchRule(normalizedText); model != "" {
		result.Signals = append(result.Signals, "custom_rule")
		// Rules don't change category, they provide direct model routing
		// The router will check for rule matches separately
	}
	
	// Score each category
	scores := make(map[TaskCategory]float64)
	signals := make(map[TaskCategory][]string)
	
	for category, cap := range c.capabilities {
		score, categorySignals := c.scoreCapability(normalizedText, text, cap)
		if score > 0 {
			scores[category] = score
			signals[category] = categorySignals
		}
	}
	
	// Find the highest scoring category
	var maxScore float64
	for category, score := range scores {
		if score > maxScore {
			maxScore = score
			result.Category = category
			result.Signals = signals[category]
		}
	}
	
	// Adjust confidence based on score
	if maxScore > 0 {
		result.Confidence = min(0.5+maxScore*0.1, 0.95)
	}
	
	// Check for simple tasks based on length
	if result.TokenEstimate < 50 && result.Category == TaskGeneral {
		// Short queries might be simple
		if hasSimplePattern(normalizedText) {
			result.Category = TaskSimple
			result.Confidence = 0.7
			result.Signals = append(result.Signals, "short_query")
		}
	}
	
	return result
}

// scoreCapability scores how well the text matches a capability.
func (c *Classifier) scoreCapability(normalizedText, originalText string, cap *Capability) (float64, []string) {
	var score float64
	var signals []string
	
	// Check keywords
	for _, keyword := range cap.Keywords {
		if strings.Contains(normalizedText, strings.ToLower(keyword)) {
			score += 1.0
			signals = append(signals, "keyword:"+keyword)
		}
	}
	
	// Check patterns
	for _, pattern := range cap.Patterns {
		if pattern.MatchString(originalText) {
			score += 2.0 // Patterns are stronger signals
			signals = append(signals, "pattern:"+pattern.String())
		}
	}
	
	// Check for code indicators
	if cap == c.capabilities[TaskCode] || (cap == nil && hasCodeIndicators(originalText)) {
		codeScore, codeSignals := detectCode(originalText)
		score += codeScore
		signals = append(signals, codeSignals...)
	}
	
	return score, signals
}

// matchRule checks if any rule matches the text.
func (c *Classifier) matchRule(normalizedText string) string {
	for _, rule := range c.rules {
		if rule.Pattern.MatchString(normalizedText) {
			return rule.Model
		}
	}
	return ""
}

// MatchRule returns the model from the first matching rule.
func (c *Classifier) MatchRule(text string) (string, bool) {
	normalizedText := strings.ToLower(text)
	model := c.matchRule(normalizedText)
	return model, model != ""
}

// detectCode detects code-related content in text.
func detectCode(text string) (float64, []string) {
	var score float64
	var signals []string
	
	// Code block indicators
	if strings.Contains(text, "```") {
		score += 3.0
		signals = append(signals, "code_block")
	}
	
	// Common programming patterns
	codePatterns := []struct {
		pattern *regexp.Regexp
		signal  string
		score   float64
	}{
		{regexp.MustCompile(`func\s+\w+\s*\(`), "go_function", 2.0},
		{regexp.MustCompile(`def\s+\w+\s*\(`), "python_function", 2.0},
		{regexp.MustCompile(`function\s+\w+\s*\(`), "js_function", 2.0},
		{regexp.MustCompile(`class\s+\w+`), "class_definition", 2.0},
		{regexp.MustCompile(`import\s+[\w.]+`), "import_statement", 1.5},
		{regexp.MustCompile(`\w+\.\w+\(`), "method_call", 1.0},
		{regexp.MustCompile(`if\s*\(.+\)\s*{`), "control_flow", 1.0},
		{regexp.MustCompile(`for\s*\(.+\)\s*{`), "for_loop", 1.0},
		{regexp.MustCompile(`\w+\s*=\s*.+;`), "assignment", 0.5},
	}
	
	for _, cp := range codePatterns {
		if cp.pattern.MatchString(text) {
			score += cp.score
			signals = append(signals, cp.signal)
		}
	}
	
	// Check for high symbol density (common in code)
	symbolCount := 0
	for _, r := range text {
		if !unicode.IsLetter(r) && !unicode.IsSpace(r) && !unicode.IsDigit(r) {
			symbolCount++
		}
	}
	symbolDensity := float64(symbolCount) / float64(len(text)+1)
	if symbolDensity > 0.1 {
		score += symbolDensity * 5
		signals = append(signals, "high_symbol_density")
	}
	
	return score, signals
}

// hasCodeIndicators checks if text has obvious code indicators.
func hasCodeIndicators(text string) bool {
	indicators := []string{"```", "function", "def ", "class ", "import ", "return ", "if (", "for ("}
	lowerText := strings.ToLower(text)
	for _, ind := range indicators {
		if strings.Contains(lowerText, ind) {
			return true
		}
	}
	return false
}

// hasSimplePattern checks if text looks like a simple question.
func hasSimplePattern(text string) bool {
	simplePatterns := []string{
		"what is",
		"what's",
		"who is",
		"who's",
		"define",
		"list",
		"name",
		"tell me",
		"quick",
	}
	for _, p := range simplePatterns {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

// estimateTokens provides a rough token estimate.
// This uses a simple heuristic: ~4 characters per token for English text.
func estimateTokens(text string) int {
	// Remove extra whitespace
	text = strings.TrimSpace(text)
	if len(text) == 0 {
		return 0
	}
	
	// Rough estimate: 4 characters per token
	return (len(text) + 3) / 4
}

// min returns the minimum of two float64 values.
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
