// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadqualification

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordLeadQualificationStepType, dex.GetFinalStepType[Input](recordLeadQualification{}))
	require.Equal(t, checkZohoDealStageStepType, dex.GetFinalStepType[crm.ListModuleFieldsResult](checkZohoDealStage{}))
	require.Equal(t, chooseZohoAccountStepType, dex.GetFinalStepType[crm.FindRecordsResult](chooseZohoAccount{}))
	require.Equal(t, recordZohoAccountStepType, dex.GetFinalStepType[crm.UpsertRecordResult](recordZohoAccount{}))
	require.Equal(t, recordZohoContactStepType, dex.GetFinalStepType[crm.UpsertRecordResult](recordZohoContact{}))
	require.Equal(t, reportZohoContactRejectedStepType, dex.GetFinalStepType[crm.UpsertRecordResult](reportZohoContactRejected{}))
	require.Equal(t, chooseZohoOpenDealStepType, dex.GetFinalStepType[crm.FindRecordsResult](chooseZohoOpenDeal{}))
	require.Equal(t, completeLeadQualificationStepType, dex.GetFinalStepType[crm.GetRecordResult](completeLeadQualification{}))
	wait, err := recordLeadQualification{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, ConnectionName))})
	require.NoError(t, err)
	require.Panics(t, func() {
		_, _ = dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, "another-connection"))})
	})
}

func TestBuildLeadQualificationRequestValidatesStartInput(t *testing.T) {
	request, err := BuildLeadQualificationRequest(Input{
		ContactEmail: " jane@acme.example.com ", ContactFirstName: " Jane ", ContactLastName: "Smith", AccountName: " Acme Corp ",
		Stage: " Negotiation/Review ", OwnerID: " 4150868000000225013 ",
	})
	require.NoError(t, err)
	require.Equal(t, Input{
		ContactEmail: "jane@acme.example.com", ContactFirstName: "Jane", ContactLastName: "Smith", AccountName: "Acme Corp",
		Stage: "Negotiation/Review", OwnerID: "4150868000000225013",
	}, request)
	valid := Input{ContactEmail: "jane@acme.example.com", ContactLastName: "Smith", AccountName: "Acme Corp", Stage: "Qualification"}
	for name, change := range map[string]func(*Input){
		"display address":    func(input *Input) { input.ContactEmail = "Jane <jane@acme.example.com>" },
		"apostrophe email":   func(input *Input) { input.ContactEmail = "o'brien@acme.example.com" },
		"no last name":       func(input *Input) { input.ContactLastName = " " },
		"apostrophe account": func(input *Input) { input.AccountName = "Macy's" },
		"no stage":           func(input *Input) { input.Stage = "" },
		"owner name":         func(input *Input) { input.OwnerID = "Patricia" },
	} {
		input := valid
		change(&input)
		_, err := BuildLeadQualificationRequest(input)
		require.Error(t, err, name)
	}
}

func TestMappersWriteZohoCRMFieldsAndQueries(t *testing.T) {
	findAccount, err := crm.BuildFindRecordsQuery(MapToFindAccountInput(AccountLookup{AccountName: "Acme Corp"}))
	require.NoError(t, err)
	require.Equal(t, "select Account_Name from Accounts where Account_Name = 'Acme Corp' order by id asc limit 0, 2", findAccount)
	findDeal, err := crm.BuildFindRecordsQuery(MapToFindOpenDealInput(OpenDealLookup{ContactID: "4150868000000376008"}))
	require.NoError(t, err)
	require.Equal(t, "select Deal_Name, Stage, Owner, Modified_Time from Deals where (Contact_Name = '4150868000000376008' and "+
		"Stage not in ('Closed Won', 'Closed Lost', 'Closed Lost to Competition')) order by Modified_Time desc, id asc limit 0, 1", findDeal)

	contact := MapToUpsertContactInput(ContactUpsert{Request: Input{ContactEmail: "jane@acme.example.com", ContactLastName: "Smith"}, AccountID: "41"})
	require.Equal(t, []string{crm.FieldEmail}, contact.DuplicateCheckFields)
	require.NotContains(t, contact.Fields, crm.FieldFirstName, "a blank first name is not sent")
	require.JSONEq(t, `{"id":"41"}`, string(contact.Fields[crm.FieldAccountName]))

	update := MapToUpdateDealInput(DealStageChange{DealID: "7", Stage: "Closed Won"})
	require.Equal(t, map[string]json.RawMessage{crm.FieldStage: crm.TextFieldValue("Closed Won")}, update.Fields, "a blank owner keeps the owner")
	readBack := MapToReadBackDealInput(crm.UpdateRecordResult{Value: crm.UpdateRecordOutput{ID: "7"}})
	require.Equal(t, crm.ModuleDeals, readBack.Module)
	require.Equal(t, "7", readBack.RecordID)
}

func newUnitTestConnection(t *testing.T, name string) crm.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "zoho", Name: name}
	client, err := crm.New(crm.Config{}, sdkgo.StaticCredentialProvider[crm.Credentials]{})
	require.NoError(t, err)
	connection, err := crm.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}
