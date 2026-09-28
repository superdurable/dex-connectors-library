// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"fmt"
	"regexp"

	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

// snapshotSuffixPattern matches the optional dated snapshot suffix of an OpenAI model ID, such as -2026-03-05.
const snapshotSuffixPattern = `(-[0-9]{4}-[0-9]{2}-[0-9]{2})?`

// responsesModelFamily holds the request limits OpenAI documents for one model family.
type responsesModelFamily struct {
	modelIDPattern   *regexp.Regexp
	reasoningEfforts map[llm.ReasoningEffort]string
	temperature      llm.TemperaturePolicy
	// isTemperatureOnlyWithoutReasoning follows OpenAI's rule that sampling
	// parameters apply only when reasoning effort is none.
	isTemperatureOnlyWithoutReasoning bool
	isReasoningOffByDefault           bool
	isStreamingUnsupported            bool
	structuredOutput                  llm.StructuredOutputRules
}

// undocumentedResponsesModelFamily sends every field, so a model OpenAI adds works before a connector release.
var undocumentedResponsesModelFamily = responsesModelFamily{
	reasoningEfforts: reasoningEffortWireValues(
		llm.ReasoningEffortNone, llm.ReasoningEffortMinimal, llm.ReasoningEffortLow, llm.ReasoningEffortMedium,
		llm.ReasoningEffortHigh, llm.ReasoningEffortExtraHigh, llm.ReasoningEffortMax,
	),
	temperature:      llm.TemperatureRange(0, 2),
	structuredOutput: llm.StructuredOutputRules{Mode: llm.StructuredOutputModeJSONSchema},
}

// documentedResponsesModelFamilies are matched in order. Each effort set is
// quoted from the model's page under https://developers.openai.com/api/docs/models.
var documentedResponsesModelFamilies = []responsesModelFamily{
	// GPT-6 Astra has no none effort, and the GPT-6 guide allows temperature only at none.
	newResponsesModelFamily(`^gpt-6-astra`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.reasoningEfforts = reasoningEffortWireValues(llm.ReasoningEffortLow, llm.ReasoningEffortMedium,
			llm.ReasoningEffortHigh, llm.ReasoningEffortExtraHigh, llm.ReasoningEffortMax)
		family.temperature = llm.TemperatureNotAccepted()
	}),
	// GPT-6 Sol and Luna default to medium effort.
	newResponsesModelFamily(`^gpt-6-(sol|luna)`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.reasoningEfforts = reasoningEffortWireValues(llm.ReasoningEffortNone, llm.ReasoningEffortLow,
			llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortExtraHigh, llm.ReasoningEffortMax)
		family.isTemperatureOnlyWithoutReasoning = true
	}),
	newResponsesModelFamily(`^gpt-5\.6-(sol|terra|luna)`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.reasoningEfforts = reasoningEffortWireValues(llm.ReasoningEffortNone, llm.ReasoningEffortLow,
			llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortExtraHigh, llm.ReasoningEffortMax)
	}),
	// GPT-5.5 Pro does not list streaming among its supported features.
	newResponsesModelFamily(`^gpt-5\.5-pro`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.reasoningEfforts = reasoningEffortWireValues(llm.ReasoningEffortMedium, llm.ReasoningEffortHigh,
			llm.ReasoningEffortExtraHigh)
		family.isStreamingUnsupported = true
	}),
	newResponsesModelFamily(`^gpt-5\.5`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.reasoningEfforts = reasoningEffortWireValues(llm.ReasoningEffortNone, llm.ReasoningEffortLow,
			llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortExtraHigh)
	}),
	newResponsesModelFamily(`^gpt-5\.[24]-pro`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.reasoningEfforts = reasoningEffortWireValues(llm.ReasoningEffortMedium, llm.ReasoningEffortHigh,
			llm.ReasoningEffortExtraHigh)
	}),
	newResponsesModelFamily(`^gpt-5\.[23]-codex`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.reasoningEfforts = reasoningEffortWireValues(llm.ReasoningEffortLow, llm.ReasoningEffortMedium,
			llm.ReasoningEffortHigh, llm.ReasoningEffortExtraHigh)
	}),
	// The GPT-5.2 and GPT-5.4 guides allow temperature only at none, their default effort.
	newResponsesModelFamily(`^gpt-5\.(2|4|4-mini|4-nano)`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.reasoningEfforts = reasoningEffortWireValues(llm.ReasoningEffortNone, llm.ReasoningEffortLow,
			llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortExtraHigh)
		family.isTemperatureOnlyWithoutReasoning = true
		family.isReasoningOffByDefault = true
	}),
	newResponsesModelFamily(`^gpt-5\.1`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.reasoningEfforts = reasoningEffortWireValues(llm.ReasoningEffortNone, llm.ReasoningEffortLow,
			llm.ReasoningEffortMedium, llm.ReasoningEffortHigh)
	}),
	newResponsesModelFamily(`^gpt-5-pro`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.reasoningEfforts = reasoningEffortWireValues(llm.ReasoningEffortHigh)
	}),
	newResponsesModelFamily(`^gpt-5(-mini|-nano)?`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.reasoningEfforts = reasoningEffortWireValues(llm.ReasoningEffortMinimal, llm.ReasoningEffortLow,
			llm.ReasoningEffortMedium, llm.ReasoningEffortHigh)
	}),
	// o1-pro and o3-pro do not list streaming among their supported features.
	newResponsesModelFamily(`^o[13]-pro`+snapshotSuffixPattern+`$`, func(family *responsesModelFamily) {
		family.isStreamingUnsupported = true
	}),
	// Structured Outputs rejects these keywords for fine-tuned models.
	newResponsesModelFamily(`^ft:.+$`, func(family *responsesModelFamily) {
		family.structuredOutput.KeywordsMovedToDescription = []string{
			"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems", "format",
		}
	}),
}

// newResponsesModelFamily starts from undocumentedResponsesModelFamily, so a family lists only what OpenAI documents.
func newResponsesModelFamily(modelIDPattern string, narrow func(*responsesModelFamily)) responsesModelFamily {
	family := undocumentedResponsesModelFamily
	family.modelIDPattern = regexp.MustCompile(modelIDPattern)
	narrow(&family)
	return family
}

// responsesModelFamilyFor returns the first documented family that matches model.
func responsesModelFamilyFor(model string) responsesModelFamily {
	for _, family := range documentedResponsesModelFamilies {
		if family.modelIDPattern.MatchString(model) {
			return family
		}
	}
	return undocumentedResponsesModelFamily
}

func (family responsesModelFamily) requestRules() llm.ModelRequestRules {
	return llm.ModelRequestRules{
		Temperature: family.temperature, ReasoningEfforts: family.reasoningEfforts, StructuredOutput: family.structuredOutput,
	}
}

// validateTemperatureWithReasoningEffort rejects a temperature that OpenAI accepts only without reasoning.
func (family responsesModelFamily) validateTemperatureWithReasoningEffort(effort llm.ReasoningEffort) error {
	if !family.isTemperatureOnlyWithoutReasoning {
		return nil
	}
	if effort == llm.ReasoningEffortNone || (effort == "" && family.isReasoningOffByDefault) {
		return nil
	}
	return fmt.Errorf("the model accepts a temperature only with reasoning effort none")
}

// reasoningEffortWireValues maps each effort to its identical Responses API reasoning.effort value.
func reasoningEffortWireValues(efforts ...llm.ReasoningEffort) map[llm.ReasoningEffort]string {
	wireValues := make(map[llm.ReasoningEffort]string, len(efforts))
	for _, effort := range efforts {
		wireValues[effort] = string(effort)
	}
	return wireValues
}
