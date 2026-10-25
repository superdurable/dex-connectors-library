//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package submissionintake

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion/notionmock"
)

func TestSubmissionIntakeUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := notionmock.New(t, ConnectionName)
	flow, harness := newNotionIntegrationHarnessWithConnection(t, mock.Connection())
	record := runSubmissionFlow(t, integrationContext(t), harness, flow, "mock-default", submissionInput("sub-new-1"))
	require.Equal(t, PhaseUpdated, record.Phase)
	require.True(t, record.IsExistingRow)
	require.Equal(t, submissionsSourceID, record.DataSourceID)
	require.Equal(t, "sub-new-1", record.StoredProperties[SubmissionIDProperty])
	require.Equal(t, "Hello from the form.\nSecond paragraph.", record.StoredBodyText)
	require.Empty(t, mock.CreatePage().Calls())
	require.NotEmpty(t, mock.UpdatePageProperties().Calls())
}
