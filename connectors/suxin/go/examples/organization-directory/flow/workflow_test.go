// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package organizationcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/suxin/go"
	"github.com/superdurable/dex-connectors-library/connectors/suxin/go/suxinmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestOrganizationCatalogFlowIdentityAndMapping(t *testing.T) {
	input := suxin.ListOrganizationsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(suxin.Connection{})))
	require.Equal(t, "StartListOrganizations", dex.GetFinalStepType[suxin.ListOrganizationsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := suxinmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
