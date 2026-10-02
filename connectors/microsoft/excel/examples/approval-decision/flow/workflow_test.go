// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvaldecision

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func policyRow(category string, limit excel.CellValue, approver string) excel.TableRow {
	return excel.TableRow{Values: map[string]excel.CellValue{
		PolicyCategoryColumn: excel.TextCellValue(category), PolicyLimitColumn: limit, PolicyApproverColumn: excel.TextCellValue(approver),
	}}
}

func TestDecideAgainstPolicyMatchesOneCategoryRow(t *testing.T) {
	rows := []excel.TableRow{
		policyRow("Travel ", excel.NumberCellValue(500), "lead@contoso.com"),
		policyRow("hardware", excel.NumberCellValue(1500), "it@contoso.com"),
		policyRow("software", excel.TextCellValue("ask"), "it@contoso.com"),
		policyRow("training", excel.NumberCellValue(100), "a"), policyRow("Training", excel.NumberCellValue(200), "b"),
	}
	decision, limit, approver, _ := DecideAgainstPolicy(Input{Category: "travel", AmountUSD: 500}, rows)
	require.Equal(t, DecisionApproved, decision)
	require.Equal(t, 500.0, limit)
	require.Empty(t, approver)
	decision, _, approver, _ = DecideAgainstPolicy(Input{Category: "hardware", AmountUSD: 1500.01}, rows)
	require.Equal(t, DecisionEscalated, decision)
	require.Equal(t, "it@contoso.com", approver)
	for _, category := range []string{"software", "training", "catering"} {
		decision, _, _, reason := DecideAgainstPolicy(Input{Category: category, AmountUSD: 1}, rows)
		require.Equal(t, DecisionNeedsReview, decision, category)
		require.NotEmpty(t, reason, category)
	}
}

func TestValidateRequestTrimsTextAndRejectsUnusableRequests(t *testing.T) {
	request, err := ValidateRequest(Input{RequestID: " REQ-1 ", Requester: "dana@contoso.com", Category: " travel", AmountUSD: 12.5})
	require.NoError(t, err)
	require.Equal(t, Input{RequestID: "REQ-1", Requester: "dana@contoso.com", Category: "travel", AmountUSD: 12.5}, request)
	for name, input := range map[string]Input{
		"blank request ID": {Requester: "a", Category: "b", AmountUSD: 1},
		"line break":       {RequestID: "REQ\n1", Requester: "a", Category: "b", AmountUSD: 1},
		"zero amount":      {RequestID: "REQ-1", Requester: "a", Category: "b"},
		"NaN amount":       {RequestID: "REQ-1", Requester: "a", Category: "b", AmountUSD: math.NaN()},
		"huge amount":      {RequestID: "REQ-1", Requester: "a", Category: "b", AmountUSD: 2e9},
	} {
		_, err := ValidateRequest(input)
		require.Error(t, err, name)
	}
}

func testConfigurations() (sdkgo.ConnectorLoadedConfiguration[TableConfiguration], sdkgo.ConnectorLoadedConfiguration[TableConfiguration], sdkgo.ConnectorLoadedConfiguration[WorksheetConfiguration]) {
	return sdkgo.ConnectorLoadedConfiguration[TableConfiguration]{Reference: PolicyTableConfigurationRef(), Value: TableConfiguration{DriveID: "b!d", WorkbookID: "W1", Table: "{P}"}},
		sdkgo.ConnectorLoadedConfiguration[TableConfiguration]{Reference: DecisionTableConfigurationRef(), Value: TableConfiguration{DriveID: "b!d", WorkbookID: "W1", Table: "{D}"}},
		sdkgo.ConnectorLoadedConfiguration[WorksheetConfiguration]{Reference: SummaryWorksheetConfigurationRef(), Value: WorksheetConfiguration{DriveID: "b!e", WorkbookID: "W2", Worksheet: "{S}"}}
}

func TestMappersUseTheSavedPicksAndOneKeyedDecisionRow(t *testing.T) {
	policy, decisions, summary := testConfigurations()
	flow := NewFlow(excel.Connection{}, policy, decisions, summary)
	decision := ApprovalDecision{
		Request:  Input{RequestID: "REQ-1", Requester: "dana@contoso.com", Category: "travel", AmountUSD: 900},
		Decision: DecisionEscalated, Approver: "lead@contoso.com", DecidedAt: "2026-10-01T12:00:00Z",
	}
	require.Equal(t, excel.GetTableRowsInput{DriveID: "b!d", WorkbookID: "W1", Table: "{P}"}, flow.MapToReadPolicyInput(decision))
	appended := flow.MapToAppendDecisionInput(decision)
	require.Equal(t, "{D}", appended.Table)
	require.Equal(t, DecisionRequestIDColumn, appended.KeyColumn)
	require.Len(t, appended.Rows, 1)
	require.Equal(t, excel.TextCellValue("REQ-1"), appended.Rows[0][DecisionRequestIDColumn])
	require.Equal(t, excel.NumberCellValue(900), appended.Rows[0][DecisionAmountColumn])
	require.Equal(t, excel.TextCellValue("2026-10-01T12:00:00Z"), appended.Rows[0][DecisionTimeColumn])
	update := flow.MapToUpdateSummaryInput(decision)
	require.Equal(t, SummaryAddress, update.Address)
	require.Equal(t, "{S}", update.Worksheet)
	require.Len(t, update.Values[0], 5)
	require.Equal(t, excel.GetValuesInput{DriveID: "b!e", WorkbookID: "W2", Worksheet: "{S}", Address: SummaryAddress},
		flow.MapToReadBackSummaryInput(excel.UpdateValuesResult{}))
}

func TestMissingPicksNameTheStepToConfigure(t *testing.T) {
	policy, decisions, summary := testConfigurations()
	require.Empty(t, NewFlow(excel.Connection{}, policy, decisions, summary).missingPickStep())
	summary.Value.Worksheet = ""
	require.Equal(t, updateSummaryStepType, NewFlow(excel.Connection{}, policy, decisions, summary).missingPickStep())
	decisions.Value.Table = ""
	require.Equal(t, appendDecisionStepType, NewFlow(excel.Connection{}, policy, decisions, summary).missingPickStep())
	policy.Value.DriveID = ""
	require.Equal(t, readPolicyStepType, NewFlow(excel.Connection{}, policy, decisions, summary).missingPickStep())
}

func TestStepIdentitiesAndRegistration(t *testing.T) {
	policy, decisions, summary := testConfigurations()
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(excel.Connection{}, policy, decisions, summary)))
	require.Equal(t, recordRequestStepType, dex.GetFinalStepType[Input](recordApprovalRequest{}))
	require.Equal(t, decideApprovalStepType, dex.GetFinalStepType[excel.GetTableRowsResult](decideApproval{}))
	require.Equal(t, completeDecisionStepType, dex.GetFinalStepType[excel.GetValuesResult](completeApprovalDecision{}))
	require.Equal(t, reportUncertainStepType, dex.GetFinalStepType[excel.AppendTableRowsResult](reportDecisionUncertain{}))

	client, err := excel.New(excel.Config{}, sdkgo.StaticCredentialProvider[excel.Credentials]{})
	require.NoError(t, err)
	connection, err := excel.NewConnection(client, sdkgo.ConnectionRef{Provider: "microsoft", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, policy, decisions, summary)})
	require.NoError(t, err)
	other, err := excel.NewConnection(client, sdkgo.ConnectionRef{Provider: "microsoft", Name: "another"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(other, policy, decisions, summary)}) })
}
