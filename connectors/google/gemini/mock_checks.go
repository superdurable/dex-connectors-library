// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

// ValidateGenerateContentMockRequest applies the real operation's provider-independent
// request checks to a scripted call. A blank model uses the manifest default.
// Provider-specific schema acceptance still belongs to Gemini.
func ValidateGenerateContentMockRequest(request GenerateContentRequest) *sdkgo.Failure {
	_, _, failure := encodeGenerateContentRequest(request, DefaultConfig().Model)
	return failure
}

// ValidateGenerateContentMockAnswer checks a generated mock answer against a
// structured-output request. It checks JSON syntax and validates the portable
// JSON Schema subset; Gemini-specific schema rules remain with the provider.
// Errors exclude prompt and answer text.
func ValidateGenerateContentMockAnswer(request GenerateContentRequest, answer GenerateContentResponse) error {
	if answer.FinishReason != "STOP" || answer.Text == "" {
		return errors.New("a generated Gemini answer needs STOP and nonempty text")
	}
	if request.ResponseJSONSchema == nil {
		return nil
	}
	if !json.Valid([]byte(answer.Text)) {
		return errors.New("a structured Gemini answer must be exactly one JSON value")
	}
	portable := textgen.TextGenerationRequest{
		Messages:         []textgen.Message{{Role: textgen.MessageRoleUser, Text: "check"}},
		StructuredOutput: &textgen.StructuredOutput{Name: "gemini-structured-output", Schema: request.ResponseJSONSchema},
	}
	if textgen.ValidatePortableRequest(portable) != nil {
		return nil
	}
	if _, err := textgen.ExtractStructuredAnswer(portable, answer.Text); err != nil {
		return fmt.Errorf("structured Gemini answer does not match the portable schema: %w", err)
	}
	return nil
}
