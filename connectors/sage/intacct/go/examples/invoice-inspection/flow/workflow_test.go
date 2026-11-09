// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package invoiceinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/sage/intacct/go"
	"github.com/superdurable/dex-connectors-library/connectors/sage/intacct/go/sageintacctmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestInvoiceInspectionFlowIdentityAndMapping(t *testing.T) {
	input := sageintacct.GetInvoiceInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(sageintacct.Connection{})))
	require.Equal(t, "StartGetInvoice", dex.GetFinalStepType[sageintacct.GetInvoiceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := sageintacctmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
