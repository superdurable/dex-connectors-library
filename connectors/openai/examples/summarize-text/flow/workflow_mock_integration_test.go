//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/connectors/openai/openaimock"
)

func TestSummarizeTextExampleRunsOnOpenAIMockDefaultsWithRealDex(t *testing.T) {
	mock := openaimock.New(t, ConnectionName)
	flow, client := startSummaryWorkerWithConnection(t, mock.Connection(), SummaryModelConfiguration{})
	flowID := fmt.Sprintf("openai-summary-mock-%d", time.Now().UnixNano())
	outcome := runSummaryFlow(t, client, flow, flowID)

	require.Equal(t, openai.CreateResponseBranchCompleted, outcome.Branch)
	require.NotEmpty(t, strings.TrimSpace(outcome.Summary))
	require.True(t, strings.HasPrefix(outcome.ResponseID, "resp_"))
	require.Equal(t, "completed", outcome.Status)
	calls := mock.CreateResponse().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, "The OpenAI connector shipped today.", calls[0].Input.Input)
}
