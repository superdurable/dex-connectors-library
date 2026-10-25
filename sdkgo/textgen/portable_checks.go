// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgen

import "github.com/superdurable/dex-connectors-library/sdkgo"

// ValidatePortableRequest checks request requirements shared by every text
// generation provider. A valid request returns nil. A failure has the same
// kind and message as a real Invoke defect; the operation fills its Provider
// and Operation fields. Model and wire format rules are checked by Invoke.
func ValidatePortableRequest(request TextGenerationRequest) *sdkgo.Failure {
	validationFailure := func(message string) *sdkgo.Failure {
		return &sdkgo.Failure{Kind: sdkgo.FailureValidation, Message: message}
	}
	if err := validateMessages(request.Messages); err != nil {
		return validationFailure(err.Error())
	}
	if request.MaxOutputTokens < 0 {
		return validationFailure("maxOutputTokens cannot be negative")
	}
	output := request.StructuredOutput
	if output == nil {
		return nil
	}
	if !structuredOutputNamePattern.MatchString(output.Name) {
		return validationFailure("structured output name must match " + structuredOutputNamePattern.String())
	}
	if output.Schema == nil {
		return validationFailure("structured output schema is required")
	}
	canonical, err := canonicalizeSchema(output.Schema)
	if err != nil {
		return validationFailure("structured output " + err.Error())
	}
	if err := checkPortableSchema(canonical); err != nil {
		return validationFailure("structured output schema is not portable " + err.Error())
	}
	return nil
}

// ValidateStructuredAnswer checks generated text against the request's
// canonical structured output schema. Plain text requests return nil.
// Surrounding whitespace and one JSON Markdown fence are accepted, as in
// the real response path. The error describes a schema mismatch without
// including generated values.
func ValidateStructuredAnswer(request TextGenerationRequest, generatedText string) error {
	if request.StructuredOutput == nil {
		return nil
	}
	canonical, err := canonicalizeSchema(request.StructuredOutput.Schema)
	if err != nil {
		return err
	}
	return validateStructuredOutputText(stripJSONFence(generatedText), canonical)
}
