// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package medialist

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/meta/instagram-business/go"
	"github.com/superdurable/dex-connectors-library/connectors/meta/instagram-business/go/instagrambusinessmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMediaListFlowIdentityAndMapping(t *testing.T) {
	input := instagrambusiness.ListMediaInput{IGUserID: "123456789"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(instagrambusiness.Connection{})))
	require.Equal(t, "StartListMedia", dex.GetFinalStepType[instagrambusiness.ListMediaInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := instagrambusinessmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
