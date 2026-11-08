// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/meta/facebook-pages/go"
	"github.com/superdurable/dex-connectors-library/connectors/meta/facebook-pages/go/facebookpagesmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPostInspectionFlowIdentityAndMapping(t *testing.T) {
	input := facebookpages.GetPostInput{PostID: "123456789_987654321"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(facebookpages.Connection{})))
	require.Equal(t, "StartGetPost", dex.GetFinalStepType[facebookpages.GetPostInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := facebookpagesmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
