// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/webflow/webflowmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestSampleFlowRegistersWithMockConnection(t *testing.T) {
	mock := webflowmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.NotNil(t, registry)
	require.Len(t, flow.GetRPCs(), 2)
}
