// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sourceinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/segment/go"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/segment/go/segmentmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSourceInspectionFlowIdentityAndMapping(t *testing.T) {
	input := segment.GetSourceInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(segment.Connection{})))
	require.Equal(t, "StartGetSource", dex.GetFinalStepType[segment.GetSourceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := segmentmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
