// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package departmentcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/qince/go"
	"github.com/superdurable/dex-connectors-library/connectors/qince/go/qincemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDepartmentCatalogFlowIdentityAndMapping(t *testing.T) {
	input := qince.ListDepartmentsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(qince.Connection{})))
	require.Equal(t, "StartListDepartments", dex.GetFinalStepType[qince.ListDepartmentsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := qincemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
