// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgen

import "github.com/superdurable/dex-connectors-library/sdkgo"

// ValidatePortableRequest checks request requirements shared by every text
// generation provider. A valid request returns nil. A failure has the same
// kind and message as a real Invoke defect; the operation fills its Provider
// and Operation fields. Model and wire format rules are checked by Invoke.
func ValidatePortableRequest(request TextGenerationRequest) *sdkgo.Failure {
	_, failure := checkPortableRequest(request)
	return failure
}

// checkPortableRequest is ValidatePortableRequest that also returns the
// canonical structured output schema, or nil for a plain text request.
func checkPortableRequest(request TextGenerationRequest) (map[string]any, *sdkgo.Failure) {
	validationFailure := func(message string) *sdkgo.Failure {
		return &sdkgo.Failure{Kind: sdkgo.FailureValidation, Message: message}
	}
	if err := validateMessages(request.Messages); err != nil {
		return nil, validationFailure(err.Error())
	}
	if request.MaxOutputTokens < 0 {
		return nil, validationFailure("maxOutputTokens cannot be negative")
	}
	output := request.StructuredOutput
	if output == nil {
		return nil, nil
	}
	if !structuredOutputNamePattern.MatchString(output.Name) {
		return nil, validationFailure("structured output name must match " + structuredOutputNamePattern.String())
	}
	if output.Schema == nil {
		return nil, validationFailure("structured output schema is required")
	}
	canonical, err := canonicalizeSchema(output.Schema)
	if err != nil {
		return nil, validationFailure("structured output " + err.Error())
	}
	if err := checkPortableSchema(canonical); err != nil {
		return nil, validationFailure("structured output schema is not portable " + err.Error())
	}
	return canonical, nil
}

// ExtractStructuredAnswer checks generated text against the request's
// canonical structured output schema and returns the JSON text without
// surrounding whitespace or one JSON Markdown fence. Plain text requests
// return the original text. Errors omit generated values.
func ExtractStructuredAnswer(request TextGenerationRequest, generatedText string) (string, error) {
	if request.StructuredOutput == nil {
		return generatedText, nil
	}
	canonical, err := canonicalizeSchema(request.StructuredOutput.Schema)
	if err != nil {
		return "", err
	}
	return extractStructuredAnswer(canonical, generatedText)
}

// extractStructuredAnswer is ExtractStructuredAnswer for a schema that is already canonical.
func extractStructuredAnswer(canonical map[string]any, generatedText string) (string, error) {
	generatedText = stripJSONFence(generatedText)
	if err := validateStructuredOutputText(generatedText, canonical); err != nil {
		return "", err
	}
	return generatedText, nil
}
