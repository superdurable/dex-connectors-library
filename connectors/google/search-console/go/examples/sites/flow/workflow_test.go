// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sites

import (
	"testing"

	"github.com/stretchr/testify/require"
	searchconsole "github.com/superdurable/dex-connectors-library/connectors/google/search-console/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/search-console/go/searchconsolemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSitesFlowIdentityAndMapping(t *testing.T) {
	input := searchconsole.ListSitesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(searchconsole.Connection{})))
	require.Equal(t, "StartListSites", dex.GetFinalStepType[searchconsole.ListSitesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := searchconsolemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
