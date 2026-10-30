//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apirequest

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/go/oauth2httpmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestAPIRequestExampleRunsOnTheOAuth2HTTPMockWithRealDex runs the example Flow on a real Worker with the
// generated oauth2httpmock connection, as an application test does.
func TestAPIRequestExampleRunsOnTheOAuth2HTTPMockWithRealDex(t *testing.T) {
	mock := oauth2httpmock.New(t, ConnectionName)
	flow, harness := newAPIRequestHarnessWithConnection(t, mock.Connection())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)

	completedFlowID := "oauth2-http-api-request-mock-" + testRunID
	_, err := harness.client.StartFlow(ctx, flow, completedFlowID, Input{Path: "/userinfo"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	completed := waitForTerminalFlow(t, ctx, harness.client, completedFlowID)
	require.Equal(t, dex.FlowCompleted, completed.Status, completed.ErrorMessage)
	var report Report
	require.NoError(t, completed.DecodeSingleOutput(&report))
	require.Equal(t, StatusCompleted, report.Status)
	require.Equal(t, 200, report.StatusCode, "the manifest default answers an unscripted call")
	require.Contains(t, report.Body, "alice")

	rejectedFlowID := "oauth2-http-api-request-mock-rejected-" + testRunID
	mock.SendRequest().ForFlow(rejectedFlowID).Respond(oauth2httpmock.SendRequestNotFound())
	_, err = harness.client.StartFlow(ctx, flow, rejectedFlowID, Input{Path: "/missing"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	rejected := waitForTerminalFlow(t, ctx, harness.client, rejectedFlowID)
	require.Equal(t, dex.FlowFailed, rejected.Status)
	require.Contains(t, rejected.ErrorMessage, "rejected")
	require.Contains(t, rejected.ErrorMessage, "no target")

	defectFlowID := "oauth2-http-api-request-mock-defect-" + testRunID
	mock.SendRequest().ForFlow(defectFlowID).Respond(oauth2httpmock.SendRequestInvalidPath())
	_, err = harness.client.StartFlow(ctx, flow, defectFlowID, Input{Path: "userinfo"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, waitForTerminalFlow(t, ctx, harness.client, defectFlowID).Status, "the unwired defect branch fails the Flow")
}
