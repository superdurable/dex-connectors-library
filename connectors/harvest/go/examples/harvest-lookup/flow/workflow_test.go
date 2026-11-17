// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvestlookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/harvest/go"
	"github.com/superdurable/dex-connectors-library/connectors/harvest/go/harvestmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestMapToListProjectsInput(t *testing.T) {
	got := MapToListProjectsInput(Request{})
	require.Equal(t, harvest.ListProjectsInput{}, got)
}

func TestMapToListClientsInput(t *testing.T) {
	require.Equal(t, harvest.ListClientsInput{PerPage: 10}, MapToListClientsInput(harvest.GetTimeEntryResult{}))
}

func TestMapToGetTimeEntryInput(t *testing.T) {
	input := harvest.ListTimeEntriesResult{Value: harvest.TimeEntryPage{TimeEntries: []harvest.TimeEntry{{ID: 4001}}}}
	require.Equal(t, harvest.GetTimeEntryInput{ID: 4001}, MapToGetTimeEntryInput(input))
}

func TestMapToGetProjectInput(t *testing.T) {
	input := harvest.ListProjectsResult{Value: harvest.ProjectPage{Projects: []harvest.Project{{ID: 3001}}}}
	require.Equal(t, harvest.GetProjectInput{ID: 3001}, MapToGetProjectInput(input))
}

func TestFlowRegistersWithMockConnection(t *testing.T) {
	mock := harvestmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
