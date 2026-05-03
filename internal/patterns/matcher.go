package patterns

import (
	"fmt"
	"os"
	fp "path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileConfig is the on-disk YAML shape for pattern lists.
type FileConfig struct {
	MatchField string        `yaml:"match_field,omitempty"`
	Patterns   []yamlPattern `yaml:"patterns,omitempty"`
	DefaultIC  bool          `yaml:"default_ignore_case,omitempty"`
}

type yamlPattern struct {
	Name       string `yaml:"name,omitempty"`
	Substring  string `yaml:"substring,omitempty"`
	Contains   string `yaml:"contains,omitempty"`
	Glob       string `yaml:"glob,omitempty"`
	Regex      string `yaml:"regex,omitempty"`
	IgnoreCase *bool  `yaml:"ignore_case,omitempty"`
}

// CompiledRule is a validated pattern evaluated at runtime.
type CompiledRule struct {
	Name string
	Kind Kind
	Raw  string
	Re   *regexp.Regexp // only KindRegex
	SG   string         // substring or glob literal
	IC   bool           // ignore case (substring/glob/regex compile flag)
}

// Kind identifies how a rule matches.
type Kind string

const (
	KindSubstring Kind = "substring"
	KindGlob      Kind = "glob"
	KindRegex     Kind = "regex"
)

// Matcher holds ordered rules loaded from YAML and/or fallback regex env.
type Matcher struct {
	DefaultIgnoreCase bool
	FileMatchField    string // optional YAML override anchor
	Rules             []CompiledRule
}

// EvalResult captures one row of the decision matrix (logging + debug UI).
type EvalResult struct {
	Name       string `json:"name"`
	Kind       Kind   `json:"kind"`
	Pattern    string `json:"pattern"`
	IgnoreCase bool   `json:"ignore_case"`
	Matched    bool   `json:"matched"`
	ErrMsg     string `json:"error,omitempty"`
}

// MatchOutcome aggregates evaluation results against a haystack string.
type MatchOutcome struct {
	Accepted   bool
	RuleIndex  int
	RuleName   string
	Evals      []EvalResult
	RulesCount int
}

// LoadYAML reads pattern definitions from path; validates all forms at load time.
func LoadYAML(path string) (Matcher, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Matcher{}, fmt.Errorf("read patterns file: %w", err)
	}
	var fc FileConfig
	if err := yaml.Unmarshal(raw, &fc); err != nil {
		return Matcher{}, fmt.Errorf("parse YAML %s: %w", path, err)
	}

	m := Matcher{DefaultIgnoreCase: fc.DefaultIC, FileMatchField: strings.TrimSpace(fc.MatchField)}
	baseIC := fc.DefaultIC

	for i, pr := range fc.Patterns {
		compiled, err := compileYAMLPattern(i, pr, baseIC)
		if err != nil {
			return Matcher{}, fmt.Errorf("%s patterns[%d] (%s): %w",
				path, i, yamlRuleLabel(pr.Name, i), err)
		}
		m.Rules = append(m.Rules, compiled)
	}
	if len(m.Rules) == 0 {
		return Matcher{}, fmt.Errorf("%s: no patterns defined under `patterns:`", path)
	}
	return m, nil
}

// FromEnvRegex wraps a single Go regexp expression as the only rule.
func FromEnvRegex(expr string) (Matcher, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return Matcher{}, fmt.Errorf("empty AUTO_ACCEPT_REGEX")
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return Matcher{}, fmt.Errorf("compile AUTO_ACCEPT_REGEX: %w", err)
	}
	m := Matcher{
		Rules: []CompiledRule{{
			Name: "AUTO_ACCEPT_REGEX",
			Kind: KindRegex,
			Raw:  expr,
			Re:   re,
		}},
	}
	return m, nil
}

// MergeMatchers returns lhs rules followed by rhs (used if both file and env existed — we won't use).

// Evaluate runs ordered rules against haystack until the first accepts.
func (m *Matcher) Evaluate(haystack string) MatchOutcome {
	var evals []EvalResult
	count := len(m.Rules)

	for i, rule := range m.Rules {
		matched, errMsg := rule.match(haystack)
		ev := EvalResult{
			Name:       rule.Name,
			Kind:       rule.Kind,
			Pattern:    rule.Raw,
			IgnoreCase: rule.IC,
			Matched:    matched,
			ErrMsg:     errMsg,
		}
		evals = append(evals, ev)
		if matched {
			return MatchOutcome{
				Accepted:   true,
				RuleIndex:  i,
				RuleName:   rule.Name,
				Evals:      evals,
				RulesCount: count,
			}
		}
	}
	return MatchOutcome{Accepted: false, Evals: evals, RulesCount: count}
}

func (r CompiledRule) match(h string) (bool, string) {
	switch r.Kind {
	case KindSubstring:
		a, b := h, r.SG
		if r.IC {
			a = strings.ToLower(a)
			b = strings.ToLower(b)
		}
		return strings.Contains(a, b), ""
	case KindGlob:
		pat := r.SG
		name := h
		if r.IC {
			pat = strings.ToLower(pat)
			name = strings.ToLower(name)
		}
		ok, err := fp.Match(pat, name)
		if err != nil {
			return false, err.Error()
		}
		return ok, ""
	case KindRegex:
		if r.Re == nil {
			return false, "internal: missing compiled regex"
		}
		return r.Re.MatchString(h), ""
	default:
		return false, "unknown kind"
	}
}

func compileYAMLPattern(i int, pr yamlPattern, defaultIC bool) (CompiledRule, error) {
	subs := strings.TrimSpace(pr.Substring)
	if subs == "" {
		subs = strings.TrimSpace(pr.Contains)
	}
	globPat := strings.TrimSpace(pr.Glob)
	regexpr := strings.TrimSpace(pr.Regex)

	n := nonEmptyCount(subs, globPat, regexpr)
	if n != 1 {
		return CompiledRule{}, fmt.Errorf("exactly one of substring (or contains), glob, or regex must be set")
	}
	ic := defaultIC
	if pr.IgnoreCase != nil {
		ic = *pr.IgnoreCase
	}
	name := strings.TrimSpace(pr.Name)
	if name == "" {
		name = fmt.Sprintf("rule-%d", i+1)
	}
	switch {
	case subs != "":
		return CompiledRule{Name: name, Kind: KindSubstring, SG: subs, Raw: subs, IC: ic}, nil
	case globPat != "":
		if _, err := fp.Match(globPat, "."); err != nil {
			return CompiledRule{}, fmt.Errorf("glob: %w", err)
		}
		return CompiledRule{Name: name, Kind: KindGlob, SG: globPat, Raw: globPat, IC: ic}, nil
	case regexpr != "":
		compiled, err := compileRegex(regexpr, ic)
		if err != nil {
			return CompiledRule{}, err
		}
		return CompiledRule{Name: name, Kind: KindRegex, SG: "", Raw: regexpr, IC: ic, Re: compiled}, nil
	default:
		return CompiledRule{}, fmt.Errorf("internal pattern selection error")
	}
}

func compileRegex(expr string, ic bool) (*regexp.Regexp, error) {
	out := expr
	if ic && !strings.Contains(expr, "(?i)") {
		out = "(?i)" + expr
	}
	re, err := regexp.Compile(out)
	if err != nil {
		return nil, fmt.Errorf("regex: %w", err)
	}
	return re, nil
}

func nonEmptyCount(parts ...string) int {
	n := 0
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			n++
		}
	}
	return n
}

func yamlRuleLabel(name string, i int) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return fmt.Sprintf("#%d", i)
}
