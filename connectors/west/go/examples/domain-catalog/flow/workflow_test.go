// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package domaincatalog

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/west/go"
	"github.com/superdurable/dex-connectors-library/connectors/west/go/westmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestDomainCatalogFlowIdentityAndMapping(t *testing.T) {
	input := west.ListDomainsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(west.Connection{})))
	require.Equal(t, "StartListDomains", dex.GetFinalStepType[west.ListDomainsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := westmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
