// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat/openaichattest"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

// xaiReasoningSplitRenderer renders a contract Usage as xAI reports it.
type xaiReasoningSplitRenderer struct {
	renderChatReply func(reply textgentest.GeneratedReply) textgentest.FakeReply
}

// newXAIProviderDialect streams replies with xAI's {"code", "error"} envelope and reasoning outside completion_tokens.
func newXAIProviderDialect(connectionModel, alternateModel string) textgentest.ProviderDialect {
	dialect := openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
		ConnectionModel: connectionModel, AlternateModel: alternateModel, IsStreaming: true,
		ErrorTokenPointers: []string{"/code"},
	})
	dialect.GeneratedReply = withXAIReasoningSplit(dialect.GeneratedReply)
	dialect.TruncatedReply = withXAIReasoningSplit(dialect.TruncatedReply)
	dialect.BlockedReply = withXAIReasoningSplit(dialect.BlockedReply)
	dialect.InterruptedStreamReply = withXAIReasoningSplit(dialect.InterruptedStreamReply)
	dialect.UnstreamedReply = withXAIReasoningSplit(dialect.UnstreamedReply)
	return dialect
}

func withXAIReasoningSplit(renderChatReply func(reply textgentest.GeneratedReply) textgentest.FakeReply) func(reply textgentest.GeneratedReply) textgentest.FakeReply {
	if renderChatReply == nil {
		return nil
	}
	return xaiReasoningSplitRenderer{renderChatReply: renderChatReply}.renderReply
}

// renderReply sends completion_tokens as OutputTokens minus ReasoningTokens, beside xAI's unchanged total_tokens.
func (renderer xaiReasoningSplitRenderer) renderReply(reply textgentest.GeneratedReply) textgentest.FakeReply {
	if reply.Usage.ReasoningTokens > reply.Usage.OutputTokens {
		panic("a contract Usage counts ReasoningTokens inside OutputTokens")
	}
	reply.Usage.OutputTokens -= reply.Usage.ReasoningTokens
	return renderer.renderChatReply(reply)
}
