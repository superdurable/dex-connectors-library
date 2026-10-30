//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package generatesummary

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini/go/geminimock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestGenerateSummaryUsesStructuredMockWithRealDex(t *testing.T) {
	mock := geminimock.New(t, ConnectionName)
	flow, harness := newSummaryIntegrationHarnessWithConnection(t, mock.Connection())
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	request := SummaryRequest{Title: "  Launch  ", Text: "The connector shipped today."}
	result := harness.runFlow(t, ctx, flow, "gemini-mock-"+strconv.FormatInt(time.Now().UnixNano(), 10), request)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome SummaryOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, StatusGenerated, outcome.Status)
	require.Equal(t, "Launch", outcome.Title, "the Flow trims the submitted title")
	require.Equal(t, &Summary{Headline: "Connector ships", Summary: "It works. Teams can use typed steps.", KeyPoints: []string{"typed", "durable"}}, outcome.Summary)
	require.Equal(t, "STOP", outcome.FinishReason)
	require.Equal(t, 42, outcome.Usage.TotalTokens)

	calls := mock.GenerateContent().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "You write concise newsletter summaries. Use only facts stated in the provided text. "+
		"Return a headline, a two-sentence summary, and up to five key points.", calls[0].Input.SystemInstruction)
	require.Equal(t, []gemini.Content{{Role: "user", Parts: []gemini.Part{{Text: "Title: Launch\n\nThe connector shipped today."}}}}, calls[0].Input.Contents)
	require.Equal(t, 4096, calls[0].Input.MaxOutputTokens)
	schema := calls[0].Input.ResponseJSONSchema
	require.Equal(t, "object", schema["type"])
	require.Equal(t, []any{"headline", "summary", "keyPoints"}, schema["required"])
	require.Equal(t, false, schema["additionalProperties"])
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	require.Len(t, properties, 3)
	require.Equal(t, map[string]any{"type": "string", "description": "A headline of at most twelve words."}, properties["headline"])
	require.Equal(t, map[string]any{"type": "string", "description": "A two-sentence summary."}, properties["summary"])
	require.Equal(t, map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 5}, properties["keyPoints"])
}
