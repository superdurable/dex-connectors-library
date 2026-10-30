// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package figmalookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/figma/go"
	"github.com/superdurable/dex-connectors-library/connectors/figma/go/figmamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestMapToGetFileInput(t *testing.T) {
	got := MapToGetFileInput(Request{})
	require.Equal(t, figma.GetFileInput{}, got)
}

func TestFlowRegistersWithMockConnection(t *testing.T) {
	mock := figmamock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
