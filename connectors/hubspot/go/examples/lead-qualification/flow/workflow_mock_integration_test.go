//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadqualification

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot/go"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot/go/hubspotmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestRecordLifecycleUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := hubspotmock.New(t, ConnectionName)
	harness := newHarnessForConnection(t, mock.Connection())
	ctx := integrationContext(t, time.Minute)
	flow := NewRecordLifecycleFlow(mock.Connection())
	flowID := uniqueFlowID("mock-record-lifecycle")
	input := hubspot.CreateObjectInput{ObjectType: hubspot.ObjectTypeDeals, Properties: map[string]string{
		"dealname": "New consulting project", "dealstage": "appointmentscheduled", "pipeline": "default",
	}}
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var archived hubspot.DeletedObject
	require.NoError(t, result.DecodeSingleOutput(&archived))
	require.Equal(t, hubspot.DeletedObject{ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9100"}, archived)
	require.Len(t, mock.CreateObject().Calls(), 1)
	require.Equal(t, input, mock.CreateObject().Calls()[0].Input)
	require.Len(t, mock.DeleteObject().Calls(), 1)
	require.Equal(t, hubspot.DeleteObjectInput{ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9100"}, mock.DeleteObject().Calls()[0].Input)
}

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
	require.Equal(t, hubspot.UpsertObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, IDProperty: "email", IDValue: existingLeadEmail,
		Properties: map[string]string{
			"firstname": "Ada", "lastname": "Lovelace", "company": "Analytical Engines", "hubspot_owner_id": integrationOwnerID,
		},
	}, upsertCalls[0].Input)
	// The example reads only the top result on purpose, so it requests one page.
	searchCalls := mock.SearchObjects().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, hubspot.SearchObjectsInput{
		ObjectType: hubspot.ObjectTypeDeals,
		FilterGroups: []hubspot.SearchFilterGroup{{Filters: []hubspot.SearchFilter{
			{PropertyName: "associations.contact", Operator: hubspot.FilterOperatorEqual, Value: "501"},
			{PropertyName: "pipeline", Operator: hubspot.FilterOperatorEqual, Value: integrationPipelineID},
			{PropertyName: "hs_is_closed", Operator: hubspot.FilterOperatorEqual, Value: "false"},
		}}},
		Sort:       &hubspot.SearchSort{PropertyName: "hs_lastmodifieddate", Direction: hubspot.SortDirectionDescending},
		Properties: []string{"dealname", "dealstage", "pipeline"},
		Limit:      1,
	}, searchCalls[0].Input)
	updateCalls := mock.UpdateObject().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, hubspot.UpdateObjectInput{
		ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9001", Properties: map[string]string{"dealstage": integrationStageID},
	}, updateCalls[0].Input)
	getCalls := mock.GetObject().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, hubspot.GetObjectInput{
		ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9001", Properties: []string{"dealname", "dealstage", "pipeline"},
	}, getCalls[0].Input)
}
