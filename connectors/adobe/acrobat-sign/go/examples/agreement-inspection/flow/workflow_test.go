// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package agreementinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/adobe/acrobat-sign/go"
	"github.com/superdurable/dex-connectors-library/connectors/adobe/acrobat-sign/go/acrobatsignmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAgreementInspectionFlowIdentityAndMapping(t *testing.T) {
	input := acrobatsign.GetAgreementInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(acrobatsign.Connection{})))
	require.Equal(t, "StartGetAgreement", dex.GetFinalStepType[acrobatsign.GetAgreementInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := acrobatsignmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
