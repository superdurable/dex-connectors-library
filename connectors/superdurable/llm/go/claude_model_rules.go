// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"regexp"

	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

// claudeStructuredOutputRules move the bounds Claude rejects, per
// https://platform.claude.com/docs/en/build-with-claude/structured-outputs, into descriptions.
var claudeStructuredOutputRules = textgen.StructuredOutputRules{
	Mode:                       textgen.StructuredOutputModeJSONSchema,
	KeywordsMovedToDescription: []string{"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems"},
}

// Effort levels per https://platform.claude.com/docs/en/build-with-claude/effort; Claude has no "none" or "minimal".
var (
	allClaudeEfforts = map[textgen.ReasoningEffort]string{
		textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortMedium: "medium", textgen.ReasoningEffortHigh: "high",
		textgen.ReasoningEffortExtraHigh: "xhigh", textgen.ReasoningEffortMax: "max",
	}
	claudeEffortsWithoutExtraHigh = map[textgen.ReasoningEffort]string{
		textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortMedium: "medium", textgen.ReasoningEffortHigh: "high",
		textgen.ReasoningEffortMax: "max",
	}
	claudeEffortsThroughHigh = map[textgen.ReasoningEffort]string{
		textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortMedium: "medium", textgen.ReasoningEffortHigh: "high",
	}
)

// currentClaudeModelRules cover Claude Opus 4.7 and later, and unlisted IDs, so new models need no release.
var currentClaudeModelRules = textgen.ModelRequestRules{
	// Models released after Claude Opus 4.6 reject every temperature except the default 1.0.
	Temperature:      textgen.TemperatureRange(1, 1),
	ReasoningEfforts: allClaudeEfforts,
	StructuredOutput: claudeStructuredOutputRules,
}

// claudeModelRule applies rules to the model IDs that pattern matches.
type claudeModelRule struct {
	pattern *regexp.Regexp
	rules   textgen.ModelRequestRules
}

// olderClaudeModelRules name active models with narrower effort or wider temperature; dated IDs match their aliases.
var olderClaudeModelRules = []claudeModelRule{
	{pattern: regexp.MustCompile(`^claude-(opus|sonnet)-4-6$`), rules: textgen.ModelRequestRules{
		Temperature: textgen.TemperatureRange(0, 1), ReasoningEfforts: claudeEffortsWithoutExtraHigh, StructuredOutput: claudeStructuredOutputRules,
	}},
	{pattern: regexp.MustCompile(`^claude-mythos-preview$`), rules: textgen.ModelRequestRules{
		Temperature: textgen.TemperatureRange(1, 1), ReasoningEfforts: claudeEffortsWithoutExtraHigh, StructuredOutput: claudeStructuredOutputRules,
	}},
	{pattern: regexp.MustCompile(`^claude-opus-4-5(-[0-9]{8})?$`), rules: textgen.ModelRequestRules{
		Temperature: textgen.TemperatureRange(0, 1), ReasoningEfforts: claudeEffortsThroughHigh, StructuredOutput: claudeStructuredOutputRules,
	}},
	{pattern: regexp.MustCompile(`^claude-(sonnet|haiku)-4-5(-[0-9]{8})?$`), rules: textgen.ModelRequestRules{
		Temperature: textgen.TemperatureRange(0, 1), StructuredOutput: claudeStructuredOutputRules,
	}},
}

// rulesForClaudeModel returns the request rules for a validated model ID.
func rulesForClaudeModel(model string) textgen.ModelRequestRules {
	for _, rule := range olderClaudeModelRules {
		if rule.pattern.MatchString(model) {
			return rule.rules
		}
	}
	return currentClaudeModelRules
}
