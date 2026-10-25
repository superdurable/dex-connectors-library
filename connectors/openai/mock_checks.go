// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

const (
	invalidCreateInputMessage = "input and a model of printable ASCII without spaces are required"
	notSerializableMessage    = "request is not JSON serializable"

	structuredAnswerMismatch = "the request sets StructuredOutput, so outputText must be exactly one JSON value valid against its schema; " +
		"script JSON text with the mock's Default or Respond, for example openaimock.CreateResponseCompleted(openai.Response{OutputText: `{...}`}, nil) " +
		"(the manifest default answers are plain text): "
)

// checkCreateInput returns the defect Failure that createResponse selects
// before it resolves credentials, when the request has no input or names a
// model that is not printable ASCII.
func checkCreateInput(input any, model string) *sdkgo.Failure {
	if input == nil || !isPrintableModelID(model) {
		failure := openAIFailure(sdkgo.FailureValidation, "createResponse", invalidCreateInputMessage)
		return &failure
	}
	return nil
}

// ValidateCreateResponseRequest checks the request rules that the real
// createResponse operation rejects locally as defect: a missing input, a
// model that is not printable ASCII without spaces, and an input or structured
// output schema that is not JSON serializable. A blank model uses the
// connection's model, so it passes. It returns the same failure kind and
// message as the real operation. OpenAI judges the schema's content, so a
// schema that is wrong for the provider is not rejected here.
func ValidateCreateResponseRequest(request CreateRequest) *sdkgo.Failure {
	model := cmp.Or(strings.TrimSpace(request.Model), DefaultConfig().Model)
	if failure := checkCreateInput(request.Input, model); failure != nil {
		return failure
	}
	_, inputErr := json.Marshal(request.Input)
	var schemaErr error
	if request.StructuredOutput != nil {
		_, schemaErr = json.Marshal(request.StructuredOutput)
	}
	if inputErr != nil || schemaErr != nil {
		failure := openAIFailure(sdkgo.FailureValidation, "createResponse", notSerializableMessage)
		return &failure
	}
	return nil
}

// ValidateCreateResponseAnswer checks a completed answer against the request.
// A plain request accepts any answer, because the real operation completes a
// Response without output text. A structured output request needs outputText
// that is exactly one JSON value, as OpenAI returns it: no Markdown fence and
// no text around it. When the schema is in the portable JSON Schema subset of
// textgen (every property required, no additional properties, one type per
// node), the value must also match the schema; other schemas are checked for
// JSON syntax only. Errors contain no answer text.
func ValidateCreateResponseAnswer(request CreateRequest, answer Response) error {
	output := request.StructuredOutput
	if output == nil {
		return nil
	}
	if !json.Valid([]byte(answer.OutputText)) {
		return errors.New(structuredAnswerMismatch + "the answer text is not exactly one JSON value")
	}
	portable := textgen.TextGenerationRequest{
		Messages:         []textgen.Message{{Role: textgen.MessageRoleUser, Text: "check"}},
		StructuredOutput: &textgen.StructuredOutput{Name: output.Name, Description: output.Description, Schema: output.Schema},
	}
	if textgen.ValidatePortableRequest(portable) != nil {
		return nil
	}
	if _, err := textgen.ExtractStructuredAnswer(portable, answer.OutputText); err != nil {
		return fmt.Errorf("%sthe answer text does not match the schema: %w", structuredAnswerMismatch, err)
	}
	return nil
}
