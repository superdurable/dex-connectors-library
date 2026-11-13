// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qrcodecatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/caoliao/go"
	"github.com/superdurable/dex-connectors-library/connectors/caoliao/go/caoliaomock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestQRCodeCatalogFlowIdentityAndMapping(t *testing.T) {
	input := caoliao.ListQRCodesInput{PageSize: 20}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(caoliao.Connection{})))
	require.Equal(t, "StartListQRCodes", dex.GetFinalStepType[caoliao.ListQRCodesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := caoliaomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
