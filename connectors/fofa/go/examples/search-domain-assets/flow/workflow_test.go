// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package domainassets

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/fofa/go"
	"github.com/superdurable/dex-connectors-library/connectors/fofa/go/fofamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestSearchDomainAssetsFlowIdentityAndMapping(t *testing.T) {
	input := fofa.SearchDomainAssetsInput{Domain: "example.com"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(fofa.Connection{})))
	require.Equal(t, "StartSearchDomainAssets", dex.GetFinalStepType[fofa.SearchDomainAssetsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := fofamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
