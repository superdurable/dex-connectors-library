// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package userproperties

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/heap/go"
	"github.com/superdurable/dex-connectors-library/connectors/heap/go/heapmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestFlowIdentityAndMapping(t *testing.T) {
	input := heap.SetUserPropertiesInput{Identity: "member-1", Properties: map[string]string{"Plan": "Pro"}}
	require.Equal(t, input, MapToOperationInput(input))
	mock := heapmock.New(t, ConnectionName)
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(mock.Connection())))
}
