// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package companyprofile

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/yuandian/go"
	"github.com/superdurable/dex-connectors-library/connectors/yuandian/go/yuandianmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetCompanyProfileFlowIdentityAndMapping(t *testing.T) {
	input := yuandian.GetCompanyProfileInput{CreditCode: "91110108MA0074PN30"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(yuandian.Connection{})))
	require.Equal(t, "StartGetCompanyProfile", dex.GetFinalStepType[yuandian.GetCompanyProfileInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := yuandianmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
