// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package issuereview

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/sentry/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestFlowIdentityAndMapping(t *testing.T) {
	require.Equal(t, "SentryIssueReview", dex.GetFinalFlowType(NewFlow(sentry.Connection{})))
	require.Equal(t, "PrepareRequest", dex.GetFinalStepType[Request](prepareRequest{}))
	require.Equal(t, "CompleteRequest", dex.GetFinalStepType[sentry.ListIssuesResult](completeRequest{}))
	request := Request(sentry.ListIssuesInput{Organization: "acme"})
	mapped := sentry.ListIssuesInput(request)
	require.Equal(t, request, Request(mapped))
	wait, err := prepareRequest{}.WaitFor(nil, request)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	reference := sdkgo.ConnectionRef{Provider: "sentry", Name: ConnectionName}
	mockConnection, err := sentry.NewConnectionWithOperations(nil, reference)
	require.Error(t, err)
	require.Equal(t, sentry.Connection{}, mockConnection)
}
