// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webpageread

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/dataeyes/go"
	"github.com/superdurable/dex-connectors-library/connectors/dataeyes/go/dataeyesmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestReadWebPageFlowIdentityAndMapping(t *testing.T) {
	input := dataeyes.ReadWebPageInput{URL: "https://example.com"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(dataeyes.Connection{})))
	require.Equal(t, "StartReadWebPage", dex.GetFinalStepType[dataeyes.ReadWebPageInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := dataeyesmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
