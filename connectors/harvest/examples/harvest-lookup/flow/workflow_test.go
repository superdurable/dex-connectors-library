// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvestlookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/harvest"
	"github.com/superdurable/dex-connectors-library/connectors/harvest/harvestmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestMapToListProjectsInput(t *testing.T) {
	got := MapToListProjectsInput(Request{})
	require.Equal(t, harvest.ListProjectsInput{}, got)
}

func TestFlowRegistersWithMockConnection(t *testing.T) {
	mock := harvestmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
