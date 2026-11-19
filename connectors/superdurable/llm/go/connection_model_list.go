// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

const (
	// maxConnectionModels bounds Config.Models, which readers show as one picker.
	maxConnectionModels = 100
	// maxContextWindowTokens bounds one Config.ModelContextWindowTokens value.
	maxContextWindowTokens = 100_000_000
)

var contextWindowTokensPattern = regexp.MustCompile(`^[0-9]{1,9}$`)

// validateConnectionModelList checks Config.Models and Config.ModelContextWindowTokens
// with the provider's model-ID rule. Its errors name entries by position, never by value.
func validateConnectionModelList(config *Config, modelIDRule textgen.ModelIDRule, connectionModel string) error {
	if len(config.Models) > maxConnectionModels {
		return fmt.Errorf("configuration models holds more than %d entries", maxConnectionModels)
	}
	listedModels := make(map[string]bool, len(config.Models))
	for index, entry := range config.Models {
		model, err := modelIDRule.ValidateModelID(entry)
		if err != nil {
			return fmt.Errorf("configuration models entry %d: %w", index+1, err)
		}
		if listedModels[model] {
			return fmt.Errorf("configuration models entry %d repeats an earlier entry", index+1)
		}
		listedModels[model] = true
	}
	if len(listedModels) > 0 {
		model, err := modelIDRule.ValidateModelID(connectionModel)
		if err != nil || !listedModels[model] {
			return fmt.Errorf("configuration model, or the provider's default model when it is blank, must be one of the models entries")
		}
	}
	windowModels := make(map[string]bool, len(config.ModelContextWindowTokens))
	for index, entry := range config.ModelContextWindowTokens {
		model, err := validateContextWindowEntry(entry, modelIDRule)
		if err != nil {
			return fmt.Errorf("configuration modelContextWindowTokens entry %d: %w", index+1, err)
		}
		if !listedModels[model] {
			return fmt.Errorf("configuration modelContextWindowTokens entry %d names a model that models does not list", index+1)
		}
		if windowModels[model] {
			return fmt.Errorf("configuration modelContextWindowTokens entry %d repeats an earlier model", index+1)
		}
		windowModels[model] = true
	}
	return nil
}

// validateContextWindowEntry returns the canonical model ID of one <model ID>=<tokens> entry.
// The last "=" separates the tokens, so a model ID may itself contain "=".
func validateContextWindowEntry(entry string, modelIDRule textgen.ModelIDRule) (string, error) {
	separator := strings.LastIndexByte(entry, '=')
	if separator < 0 {
		return "", fmt.Errorf("entry must be written as <model ID>=<tokens>")
	}
	model, err := modelIDRule.ValidateModelID(entry[:separator])
	if err != nil {
		return "", err
	}
	tokens := strings.TrimSpace(entry[separator+1:])
	if !contextWindowTokensPattern.MatchString(tokens) {
		return "", fmt.Errorf("tokens must be a whole number from 1 to %d", maxContextWindowTokens)
	}
	value, err := strconv.ParseInt(tokens, 10, 64)
	if err != nil || value < 1 || value > maxContextWindowTokens {
		return "", fmt.Errorf("tokens must be a whole number from 1 to %d", maxContextWindowTokens)
	}
	return model, nil
}
