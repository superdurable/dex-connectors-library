// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package claude

import (
	"regexp"

	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

// structuredOutputRules move the bounds Claude rejects, per
// https://platform.claude.com/docs/en/build-with-claude/structured-outputs, into descriptions.
var structuredOutputRules = llm.StructuredOutputRules{
	Mode:                       llm.StructuredOutputModeJSONSchema,
	KeywordsMovedToDescription: []string{"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems"},
}

// Effort levels per https://platform.claude.com/docs/en/build-with-claude/effort; Claude has no "none" or "minimal".
var (
	allEfforts = map[llm.ReasoningEffort]string{
		llm.ReasoningEffortLow: "low", llm.ReasoningEffortMedium: "medium", llm.ReasoningEffortHigh: "high",
		llm.ReasoningEffortExtraHigh: "xhigh", llm.ReasoningEffortMax: "max",
	}
	effortsWithoutExtraHigh = map[llm.ReasoningEffort]string{
		llm.ReasoningEffortLow: "low", llm.ReasoningEffortMedium: "medium", llm.ReasoningEffortHigh: "high",
		llm.ReasoningEffortMax: "max",
	}
	effortsThroughHigh = map[llm.ReasoningEffort]string{
		llm.ReasoningEffortLow: "low", llm.ReasoningEffortMedium: "medium", llm.ReasoningEffortHigh: "high",
	}
)

// currentModelRules cover Claude Opus 4.7 and later, and unlisted IDs, so new models need no release.
var currentModelRules = llm.ModelRequestRules{
	// Models released after Claude Opus 4.6 reject every temperature except the default 1.0.
	Temperature:      llm.TemperatureRange(1, 1),
	ReasoningEfforts: allEfforts,
	StructuredOutput: structuredOutputRules,
}

// modelRule applies rules to the model IDs that pattern matches.
type modelRule struct {
	pattern *regexp.Regexp
	rules   llm.ModelRequestRules
}

// olderModelRules name active models with narrower effort or wider temperature; dated IDs match their aliases.
var olderModelRules = []modelRule{
	{pattern: regexp.MustCompile(`^claude-(opus|sonnet)-4-6$`), rules: llm.ModelRequestRules{
		Temperature: llm.TemperatureRange(0, 1), ReasoningEfforts: effortsWithoutExtraHigh, StructuredOutput: structuredOutputRules,
	}},
	{pattern: regexp.MustCompile(`^claude-mythos-preview$`), rules: llm.ModelRequestRules{
		Temperature: llm.TemperatureRange(1, 1), ReasoningEfforts: effortsWithoutExtraHigh, StructuredOutput: structuredOutputRules,
	}},
	{pattern: regexp.MustCompile(`^claude-opus-4-5(-[0-9]{8})?$`), rules: llm.ModelRequestRules{
		Temperature: llm.TemperatureRange(0, 1), ReasoningEfforts: effortsThroughHigh, StructuredOutput: structuredOutputRules,
	}},
	{pattern: regexp.MustCompile(`^claude-(sonnet|haiku)-4-5(-[0-9]{8})?$`), rules: llm.ModelRequestRules{
		Temperature: llm.TemperatureRange(0, 1), StructuredOutput: structuredOutputRules,
	}},
}

// rulesForModel returns the request rules for a validated model ID.
func rulesForModel(model string) llm.ModelRequestRules {
	for _, rule := range olderModelRules {
		if rule.pattern.MatchString(model) {
			return rule.rules
		}
	}
	return currentModelRules
}
