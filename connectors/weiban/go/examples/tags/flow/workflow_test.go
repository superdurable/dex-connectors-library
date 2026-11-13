// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tags

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/weiban/go"
	"github.com/superdurable/dex-connectors-library/connectors/weiban/go/weibanmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestTagsFlowIdentityAndMapping(t *testing.T) {
	input := weiban.ListTagsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(weiban.Connection{})))
	require.Equal(t, "StartListTags", dex.GetFinalStepType[weiban.ListTagsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := weibanmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
