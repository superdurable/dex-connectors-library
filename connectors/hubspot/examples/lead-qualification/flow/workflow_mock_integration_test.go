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
	require.Equal(t, "9001", qualification.DealID)

	upsertCalls := mock.UpsertObject().Calls()
	require.Len(t, upsertCalls, 1)
	require.Equal(t, "email", upsertCalls[0].Input.IDProperty)
	require.Equal(t, existingLeadEmail, upsertCalls[0].Input.IDValue)
	// The example reads only the top result on purpose, so it requests one page.
	searchCalls := mock.SearchObjects().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, "501", searchCalls[0].Input.FilterGroups[0].Filters[0].Value, "the search filters on the contact the upsert answered")
	updateCalls := mock.UpdateObject().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, "9001", updateCalls[0].Input.ObjectID)
	require.Equal(t, integrationStageID, updateCalls[0].Input.Properties["dealstage"])
}
