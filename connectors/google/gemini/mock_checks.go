// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ValidateGenerateContentMockRequest applies the real operation's provider-independent
// request checks to a scripted call. A blank model uses the manifest default.
// Provider-specific schema acceptance still belongs to Gemini.
func ValidateGenerateContentMockRequest(request GenerateContentRequest) *sdkgo.Failure {
	_, _, failure := encodeGenerateContentRequest(request, DefaultConfig().Model)
	return failure
}

// ValidateGenerateContentMockAnswer checks the generated branch's nonempty text
// and any explicit finish reason. An omitted reason is allowed in scripts.
// Application Flows validate structured content they consume.
func ValidateGenerateContentMockAnswer(_ GenerateContentRequest, answer GenerateContentResponse) error {
	if answer.FinishReason != "" && answer.FinishReason != "STOP" {
		return errors.New("a generated Gemini answer needs STOP when a finish reason is set")
	}
	if answer.Text == "" {
		return errors.New("a generated Gemini answer needs nonempty text")
	}
	return nil
}
