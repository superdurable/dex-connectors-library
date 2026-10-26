//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package refunddecision

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/airtablemock"
)

func TestRefundDecisionUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := airtablemock.New(t, ConnectionName)
	harness := newRefundDecisionHarnessWithConnection(t, mock.Connection())
	ctx := integrationContext(t, time.Minute)
	decision := waitForCompletedDecision(t, ctx, harness,
		startDecision(t, ctx, harness, "mock-approved",
			Input{CaseID: "case-approved", Customer: "Jane Doe", AmountUSD: 120, PolicyKey: "refund-standard"}))

	require.Equal(t, PhaseCompleted, decision.Phase)
	require.Equal(t, DecisionApproved, decision.Decision)
	require.Equal(t, 1, decision.MatchingPolicyCount)
	require.Greater(t, decision.ApprovalLimitUSD, decision.Request.AmountUSD)
	require.True(t, decision.IsLogRecordCreated)
	require.True(t, decision.IsPolicyStamped, "the Flow confirms its case stamp from the update answer")
	lists := mock.ListRecords().Calls()
	require.Len(t, lists, 1)
	require.Equal(t, airtable.ListRecordsInput{
		BaseID: "appRefundBase0001", TableIDOrName: "tblPolicies000001",
		Filters: []airtable.FieldEqualityFilter{airtable.TextFieldEquals(PolicyKeyField, "refund-standard")},
		Fields:  []string{PolicyKeyField, ApprovalLimitField}, PageSize: 2,
	}, lists[0].Input)
	upserts := mock.UpsertRecords().Calls()
	require.Len(t, upserts, 1)
	require.Equal(t, airtable.UpsertRecordsInput{
		BaseID: "appRefundBase0001", TableIDOrName: "tblDecisionLog001", FieldsToMergeOn: []string{CaseIDField},
		Records: []airtable.RecordUpsert{{Fields: map[string]airtable.CellValue{
			CaseIDField: airtable.TextCellValue("case-approved"), CustomerField: airtable.TextCellValue("Jane Doe"),
			AmountField: airtable.NumberCellValue(120), DecisionField: airtable.TextCellValue("approved"),
			PolicyLinkField: airtable.LinkedRecordsCellValue("recPolicyStandard"),
		}}},
	}, upserts[0].Input)
	updates := mock.UpdateRecords().Calls()
	require.Len(t, updates, 1)
	require.Equal(t, airtable.UpdateRecordsInput{
		BaseID: "appRefundBase0001", TableIDOrName: "tblPolicies000001",
		Records: []airtable.RecordUpdate{{ID: "recPolicyStandard", Fields: map[string]airtable.CellValue{
			LastDecidedCaseField: airtable.TextCellValue("case-approved"),
		}}},
	}, updates[0].Input)
	reads := mock.GetRecord().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, airtable.GetRecordInput{
		BaseID: "appRefundBase0001", TableIDOrName: "tblDecisionLog001", RecordID: "recDecisionLog001",
	}, reads[0].Input)
}
