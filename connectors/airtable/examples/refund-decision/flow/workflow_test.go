// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package refunddecision

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testSettings = Settings{
	PolicyTable: TableSelection{BaseID: "appRefundBase0001", BaseName: "Refunds", TableID: "tblPolicies000001", TableName: "Policies"},
	LogTable:    TableSelection{BaseID: "appRefundBase0001", BaseName: "Refunds", TableID: "tblDecisionLog001", TableName: "Decision Log"},
}

func TestMapToFindRefundPolicyInputFiltersByKeyAndRevealsADuplicateKey(t *testing.T) {
	flow := mustNewFlow(t, airtable.Connection{}, &testSettings)
	input := flow.MapToFindRefundPolicyInput(RefundDecision{Request: Input{PolicyKey: "refund-standard"}})
	require.Equal(t, airtable.ListRecordsInput{
		BaseID: "appRefundBase0001", TableIDOrName: "tblPolicies000001",
		Filters:  []airtable.FieldEqualityFilter{airtable.TextFieldEquals(PolicyKeyField, "refund-standard")},
		Fields:   []string{PolicyKeyField, ApprovalLimitField},
		PageSize: 2,
	}, input, "two rows are enough to tell one match from a duplicate key")
}

func TestMapToUpsertDecisionLogInputMergesOnCaseIDAndLinksOnlyAMatchedPolicy(t *testing.T) {
	flow := mustNewFlow(t, airtable.Connection{}, &testSettings)
	decision := RefundDecision{
		Request:        Input{CaseID: "case-4471", Customer: "Jane Doe", AmountUSD: 120, PolicyKey: "refund-standard"},
		PolicyRecordID: "recPolicyStandard", Decision: DecisionApproved,
	}
	input := flow.MapToUpsertDecisionLogInput(decision)
	require.Equal(t, "tblDecisionLog001", input.TableIDOrName)
	require.Equal(t, []string{CaseIDField}, input.FieldsToMergeOn)
	require.Len(t, input.Records, 1)
	require.Equal(t, map[string]airtable.CellValue{
		CaseIDField: airtable.TextCellValue("case-4471"), CustomerField: airtable.TextCellValue("Jane Doe"),
		AmountField: airtable.NumberCellValue(120), DecisionField: airtable.TextCellValue("approved"),
		PolicyLinkField: airtable.LinkedRecordsCellValue("recPolicyStandard"),
	}, input.Records[0].Fields)

	decision.PolicyRecordID, decision.Decision = "", DecisionNeedsReview
	unlinked := flow.MapToUpsertDecisionLogInput(decision)
	require.Equal(t, airtable.LinkedRecordsCellValue(), unlinked.Records[0].Fields[PolicyLinkField], "a rerun without a match removes a stale link")
}

func TestMapToStampAndReadBackUseTheMatchedPolicyAndTheLogRow(t *testing.T) {
	flow := mustNewFlow(t, airtable.Connection{}, &testSettings)
	decision := RefundDecision{Request: Input{CaseID: "case-4471"}, PolicyRecordID: "recPolicyStandard", LogRecordID: "recDecisionLog001"}
	require.Equal(t, airtable.UpdateRecordsInput{
		BaseID: "appRefundBase0001", TableIDOrName: "tblPolicies000001",
		Records: []airtable.RecordUpdate{{ID: "recPolicyStandard", Fields: map[string]airtable.CellValue{
			LastDecidedCaseField: airtable.TextCellValue("case-4471"),
		}}},
	}, flow.MapToStampRefundPolicyInput(decision))
	require.Equal(t, airtable.GetRecordInput{
		BaseID: "appRefundBase0001", TableIDOrName: "tblDecisionLog001", RecordID: "recDecisionLog001",
	}, flow.MapToReadBackDecisionLogInput(decision))
}

func TestDecideAgainstPoliciesApprovesOnlyWithinOneMatchingLimit(t *testing.T) {
	policy := func(recordID string, fields map[string]airtable.CellValue) airtable.Record {
		return airtable.Record{ID: recordID, Fields: fields}
	}
	withLimit := policy("recPolicyStandard", map[string]airtable.CellValue{ApprovalLimitField: airtable.NumberCellValue(250)})
	for name, testCase := range map[string]struct {
		amount         float64
		policies       []airtable.Record
		decision       Decision
		policyRecordID string
	}{
		"within the limit":     {250, []airtable.Record{withLimit}, DecisionApproved, "recPolicyStandard"},
		"over the limit":       {250.01, []airtable.Record{withLimit}, DecisionEscalated, "recPolicyStandard"},
		"no policy":            {10, nil, DecisionNeedsReview, ""},
		"duplicate policy key": {10, []airtable.Record{withLimit, withLimit}, DecisionNeedsReview, ""},
		"no approval limit": {10, []airtable.Record{policy("recPolicyStandard", map[string]airtable.CellValue{
			ApprovalLimitField: airtable.TextCellValue("250"),
		})}, DecisionNeedsReview, ""},
	} {
		t.Run(name, func(t *testing.T) {
			decision := decideAgainstPolicies(RefundDecision{Request: Input{AmountUSD: testCase.amount}}, testCase.policies)
			require.Equal(t, testCase.decision, decision.Decision)
			require.Equal(t, testCase.policyRecordID, decision.PolicyRecordID)
			require.Equal(t, PhaseDecided, decision.Phase)
			require.Equal(t, len(testCase.policies), decision.MatchingPolicyCount)
			if testCase.decision == DecisionNeedsReview {
				require.NotEmpty(t, decision.Reason)
			}
		})
	}
}

func TestConfirmStoredDecisionComparesCaseDecisionAndPolicyLink(t *testing.T) {
	decision := RefundDecision{Request: Input{CaseID: "case-4471"}, Decision: DecisionApproved, PolicyRecordID: "recPolicyStandard"}
	stored := airtable.Record{ID: "recDecisionLog001", Fields: map[string]airtable.CellValue{
		CaseIDField: airtable.TextCellValue("case-4471"), DecisionField: airtable.TextCellValue("approved"),
		PolicyLinkField: airtable.LinkedRecordsCellValue("recPolicyStandard"),
	}}
	require.NoError(t, confirmStoredDecision(decision, stored))
	stored.Fields[DecisionField] = airtable.TextCellValue("escalated")
	require.Error(t, confirmStoredDecision(decision, stored))
	stored.Fields[DecisionField] = airtable.TextCellValue("approved")
	delete(stored.Fields, PolicyLinkField)
	require.Error(t, confirmStoredDecision(decision, stored), "a missing policy link is not the stored decision")
	decision.PolicyRecordID = ""
	require.NoError(t, confirmStoredDecision(decision, stored), "Airtable omits an empty link field")
}

func TestNewFlowRequiresBothTablesInOneBase(t *testing.T) {
	_, err := NewFlow(airtable.Connection{}, nil)
	require.Error(t, err)
	missingTable := testSettings
	missingTable.LogTable.TableID = ""
	_, err = NewFlow(airtable.Connection{}, &missingTable)
	require.ErrorContains(t, err, "saved in Dex Web")
	otherBase := testSettings
	otherBase.LogTable.BaseID = "appOtherBase00001"
	_, err = NewFlow(airtable.Connection{}, &otherBase)
	require.ErrorContains(t, err, "within one base")
}

func TestValidateRefundRequestTrimsAndRejectsUnfilterableValues(t *testing.T) {
	request, err := validateRefundRequest(Input{CaseID: " case-4471 ", Customer: " Jane ", AmountUSD: 120, PolicyKey: " refund-standard "})
	require.NoError(t, err)
	require.Equal(t, Input{CaseID: "case-4471", Customer: "Jane", AmountUSD: 120, PolicyKey: "refund-standard"}, request)
	for name, input := range map[string]Input{
		"blank case":              {PolicyKey: "refund-standard"},
		"blank policy key":        {CaseID: "case-1"},
		"backslash in policy key": {CaseID: "case-1", PolicyKey: `refund\standard`},
		"line break in customer":  {CaseID: "case-1", PolicyKey: "refund-standard", Customer: "a\nb"},
		"negative amount":         {CaseID: "case-1", PolicyKey: "refund-standard", AmountUSD: -1},
		"NaN amount":              {CaseID: "case-1", PolicyKey: "refund-standard", AmountUSD: math.NaN()},
	} {
		_, err := validateRefundRequest(input)
		require.Error(t, err, name)
	}
}

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := mustNewFlow(t, airtable.Connection{}, &testSettings)
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordRefundRequestStepType, dex.GetFinalStepType[Input](recordRefundRequest{}))
	require.Equal(t, decideRefundStepType, dex.GetFinalStepType[airtable.ListRecordsResult](decideRefund{}))
	require.Equal(t, recordDecisionLogStepType, dex.GetFinalStepType[airtable.UpsertRecordsResult](recordDecisionLog{}))
	require.Equal(t, recordPolicyStampStepType, dex.GetFinalStepType[airtable.UpdateRecordsResult](recordPolicyStamp{}))
	require.Equal(t, completeRefundDecisionStepType, dex.GetFinalStepType[airtable.GetRecordResult](completeRefundDecision{}))
	wait, err := recordRefundRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestConfigurationReferencesNameTheirOperationSteps(t *testing.T) {
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: airtable.ConnectorID, ConnectionName: ConnectionName, OperationID: "listRecords",
		FlowType: FlowType, StepType: findRefundPolicyStepType,
	}, PolicyTableConfigurationRef())
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: airtable.ConnectorID, ConnectionName: ConnectionName, OperationID: "upsertRecords",
		FlowType: FlowType, StepType: upsertDecisionLogStepType,
	}, LogTableConfigurationRef())
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := airtable.New(airtable.Config{}, sdkgo.StaticCredentialProvider[airtable.Credentials]{})
	require.NoError(t, err)
	connection, err := airtable.NewConnection(client, sdkgo.ConnectionRef{Provider: "airtable", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{mustNewFlow(t, connection, &testSettings)})
	require.NoError(t, err)

	otherConnection, err := airtable.NewConnection(client, sdkgo.ConnectionRef{Provider: "airtable", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{mustNewFlow(t, otherConnection, &testSettings)}) })
}

func mustNewFlow(t *testing.T, connection airtable.Connection, settings *Settings) *Flow {
	t.Helper()
	flow, err := NewFlow(connection, settings)
	require.NoError(t, err)
	return flow
}
