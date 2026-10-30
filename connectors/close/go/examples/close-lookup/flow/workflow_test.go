// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package closelookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/close/go"
	"github.com/superdurable/dex-connectors-library/connectors/close/go/closemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestMapToListLeadsInput(t *testing.T) {
	got := MapToListLeadsInput(Request{})
	require.Equal(t, close.ListLeadsInput{Limit: 10}, got)
}

func TestFlowRegistersWithMockConnection(t *testing.T) {
	mock := closemock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
