// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package companyprofile

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tianyancha/go"
	"github.com/superdurable/dex-connectors-library/connectors/tianyancha/go/tianyanchamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetCompanyProfileFlowIdentityAndMapping(t *testing.T) {
	input := tianyancha.GetCompanyProfileInput{Keyword: "平安银行股份有限公司"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(tianyancha.Connection{})))
	require.Equal(t, "StartGetCompanyProfile", dex.GetFinalStepType[tianyancha.GetCompanyProfileInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := tianyanchamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
