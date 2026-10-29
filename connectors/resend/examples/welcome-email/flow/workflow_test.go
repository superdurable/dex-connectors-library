// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package welcomeemail

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/resend"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestFlowIdentityAndMapping(t *testing.T) {
	require.Equal(t, "ResendWelcomeEmail", dex.GetFinalFlowType(NewFlow(resend.Connection{})))
	require.Equal(t, "PrepareRequest", dex.GetFinalStepType[Request](prepareRequest{}))
	require.Equal(t, "CompleteRequest", dex.GetFinalStepType[resend.SendEmailResult](completeRequest{}))
	request := Request(resend.SendEmailInput{From: "Acme <hello@example.test>", To: []string{"reader@example.test"}, Subject: "Welcome", HTML: "<p>Hello</p>"})
	mapped := resend.SendEmailInput(request)
	require.Equal(t, request, Request(mapped))
	wait, err := prepareRequest{}.WaitFor(nil, request)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	reference := sdkgo.ConnectionRef{Provider: "resend", Name: ConnectionName}
	mockConnection, err := resend.NewConnectionWithOperations(nil, reference)
	require.Error(t, err)
	require.Equal(t, resend.Connection{}, mockConnection)
}
