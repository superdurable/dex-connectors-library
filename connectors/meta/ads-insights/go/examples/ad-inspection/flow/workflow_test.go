// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/meta/ads-insights/go"
	"github.com/superdurable/dex-connectors-library/connectors/meta/ads-insights/go/metaadsinsightsmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAdInspectionFlowIdentityAndMapping(t *testing.T) {
	input := metaadsinsights.GetAdInput{AdID: "123456789012345"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(metaadsinsights.Connection{})))
	require.Equal(t, "StartGetAd", dex.GetFinalStepType[metaadsinsights.GetAdInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := metaadsinsightsmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
