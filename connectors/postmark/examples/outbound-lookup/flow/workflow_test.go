// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outboundlookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/postmark"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestFlowIdentityAndMapping(t *testing.T) {
	require.Equal(t, "PostmarkOutboundLookup", dex.GetFinalFlowType(NewFlow(postmark.Connection{})))
	require.Equal(t, "PrepareRequest", dex.GetFinalStepType[Request](prepareRequest{}))
	require.Equal(t, "CompleteRequest", dex.GetFinalStepType[postmark.ListOutboundMessagesResult](completeRequest{}))
	request := Request(postmark.ListOutboundMessagesInput{Count: 25})
	mapped := postmark.ListOutboundMessagesInput(request)
	require.Equal(t, request, Request(mapped))
	wait, err := prepareRequest{}.WaitFor(nil, request)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	reference := sdkgo.ConnectionRef{Provider: "postmark", Name: ConnectionName}
	mockConnection, err := postmark.NewConnectionWithOperations(nil, reference)
	require.Error(t, err)
	require.Equal(t, postmark.Connection{}, mockConnection)
}
