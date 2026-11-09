// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package signaturerequestinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/dropbox/sign/go"
	"github.com/superdurable/dex-connectors-library/connectors/dropbox/sign/go/dropboxsignmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSignatureRequestInspectionFlowIdentityAndMapping(t *testing.T) {
	input := dropboxsign.GetSignatureRequestInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(dropboxsign.Connection{})))
	require.Equal(t, "StartGetSignatureRequest", dex.GetFinalStepType[dropboxsign.GetSignatureRequestInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := dropboxsignmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
