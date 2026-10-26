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

	scriptStructuredAnswer = "script JSON text with the mock's Default or Respond, for example " +
		"openaimock.CreateResponseCompleted(openai.Response{Status: \"completed\", OutputText: `{...}`}, nil)"
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
// The real operation completes only a Response whose Status is completed, so
// an answer with another explicit Status is rejected for every request; an
// unset Status in a scripted answer passes. A plain request accepts any other
// answer. The real operation also delivers a refusal with empty outputText on
// the completed branch, so the structured output check runs only for an
// answer whose outputText is not empty. That answer needs outputText that
// is exactly one JSON value, as OpenAI returns it: no Markdown fence and no
// text around it. OpenAI guarantees schema adherence only with Strict, so
// only then, and when the schema is in the portable JSON Schema subset of
// textgen (every property required, no additional properties, one type per
// node), must the value also match the schema. Otherwise only the JSON syntax
// is checked. Errors contain no answer text.
func ValidateCreateResponseAnswer(request CreateRequest, answer Response) error {
	if answer.Status != "" && answer.Status != "completed" {
		return fmt.Errorf("a completed answer must have Status completed or none, but it has %q; "+
			"the real operation selects failed for a failed or incomplete Response, so script it with openaimock.CreateResponseFailed", answer.Status)
	}
	output := request.StructuredOutput
	if output == nil || answer.OutputText == "" {
		return nil
	}
	if !json.Valid([]byte(answer.OutputText)) {
		return errors.New("the request sets StructuredOutput, so a completed answer's outputText must be exactly one JSON value, " +
			"but it is plain text, fenced, or has text around the value; " + scriptStructuredAnswer + " (the manifest default answers are plain text)")
	}
	if !output.Strict {
		return nil
	}
	portable := textgen.TextGenerationRequest{
		Messages:         []textgen.Message{{Role: textgen.MessageRoleUser, Text: "check"}},
		StructuredOutput: &textgen.StructuredOutput{Name: output.Name, Description: output.Description, Schema: output.Schema},
	}
	if textgen.ValidatePortableRequest(portable) != nil {
		return nil
	}
	if _, err := textgen.ExtractStructuredAnswer(portable, answer.OutputText); err != nil {
		return fmt.Errorf("the request sets StructuredOutput with Strict, so a completed answer's outputText must match its schema: %w; %s",
			err, scriptStructuredAnswer)
	}
	return nil
}
