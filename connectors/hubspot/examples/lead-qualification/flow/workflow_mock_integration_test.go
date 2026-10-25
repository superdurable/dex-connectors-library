//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadqualification

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot/hubspotmock"
)

func TestLeadQualificationUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := hubspotmock.New(t, ConnectionName)
	harness := newHarnessForConnection(t, mock.Connection())
	ctx := integrationContext(t, time.Minute)
	flowID := startQualification(t, ctx, harness, "mock-default", Input{Email: existingLeadEmail, FirstName: "Ada", LastName: "Lovelace", Company: "Analytical Engines"})
	qualification := waitForCompletedQualification(t, ctx, harness, flowID)
	require.Equal(t, PhaseDealAdvanced, qualification.Phase)
	require.Equal(t, "501", qualification.ContactID)
	require.Equal(t, "9001", qualification.DealID)
	require.Equal(t, integrationStageID, qualification.DealStageID)
	require.NotEmpty(t, mock.UpsertObject().Calls())
	require.NotEmpty(t, mock.UpdateObject().Calls())
}
