// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tagread

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/jiguang/go"
	"github.com/superdurable/dex-connectors-library/connectors/jiguang/go/jiguangmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTagReadFlowIdentityAndMapping(t *testing.T) {
	input := jiguang.ListApplicationTagsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(jiguang.Connection{})))
	require.Equal(t, "StartListApplicationTags", dex.GetFinalStepType[jiguang.ListApplicationTagsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := jiguangmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
