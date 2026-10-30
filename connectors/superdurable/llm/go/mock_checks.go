// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"errors"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

// ValidateGenerateTextRequest checks the provider-independent request rules before a mock answers.
// For a request whose model ID passes the real connector's model validation, a rejection has the
// failure kind and message of the real generateText operation, which checks the model ID first.
// Model, feature, and wire-format rules are not checked, and the mock fills the Failure's
// Provider with llm instead of the connection's provider.
func ValidateGenerateTextRequest(request GenerateTextRequest) *sdkgo.Failure {
	return textgen.ValidatePortableRequest(request)
}

// ValidateGenerateTextAnswer checks that a generated mock answer is one the real operation returns:
// it has text, and for a structured output request it is exactly the JSON that matches the schema.
// Errors contain no generated answer text.
func ValidateGenerateTextAnswer(request GenerateTextRequest, answer GenerateTextResponse) error {
	if answer.Text == "" {
		return errors.New("the answer has no text, which the real operation reports as invalidResponse (provider finished without text); " +
			"script an answer with non-empty Text")
	}
	extracted, err := textgen.ExtractStructuredAnswer(request, answer.Text)
	if err != nil {
		return fmt.Errorf("the answer, scripted or the manifest default, does not match the request's structured output schema (%w); "+
			"the test must script a JSON answer for that schema: "+
			"mock.GenerateText().ForFlow(flowID).Respond(llmmock.GenerateTextGenerated(llm.GenerateTextResponse{Text: jsonForTheSchema}, nil))", err)
	}
	if extracted != answer.Text {
		return errors.New("the answer has a Markdown fence or surrounding whitespace around its JSON, which the real operation removes; " +
			"script the JSON text alone so the application receives what production returns")
	}
	return nil
}
