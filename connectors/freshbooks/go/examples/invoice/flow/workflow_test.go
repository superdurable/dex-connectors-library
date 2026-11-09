// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package invoice

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshbooks/go"
	"github.com/superdurable/dex-connectors-library/connectors/freshbooks/go/freshbooksmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestInvoiceFlowIdentityAndMapping(t *testing.T) {
	input := freshbooks.GetInvoiceInput{AccountID: "KAG77", InvoiceID: 2201278}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(freshbooks.Connection{})))
	require.Equal(t, "StartGetInvoice", dex.GetFinalStepType[freshbooks.GetInvoiceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := freshbooksmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
