// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newhireonboarding

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordNewHireStepType, dex.GetFinalStepType[Input](recordNewHire{}))
	require.Equal(t, adoptExistingEmployeeStepType, dex.GetFinalStepType[bamboohr.FindEmployeeByEmailResult](adoptExistingEmployee{}))
	require.Equal(t, recordAmbiguousHireStepType, dex.GetFinalStepType[bamboohr.FindEmployeeByEmailResult](recordAmbiguousHire{}))
	require.Equal(t, prepareNewHireRecordStepType, dex.GetFinalStepType[bamboohr.FindEmployeeByEmailResult](prepareNewHireRecord{}))
	require.Equal(t, adoptAddedEmployeeStepType, dex.GetFinalStepType[bamboohr.AddEmployeeResult](adoptAddedEmployee{}))
	require.Equal(t, recordUncertainHireStepType, dex.GetFinalStepType[bamboohr.AddEmployeeResult](recordUncertainHire{}))
	require.Equal(t, adoptReconciledEmployeeStepType, dex.GetFinalStepType[bamboohr.FindEmployeeByEmailResult](adoptReconciledEmployee{}))
	require.Equal(t, recordUnreconciledHireStepType, dex.GetFinalStepType[bamboohr.FindEmployeeByEmailResult](recordUnreconciledHire{}))
	require.Equal(t, checkOnboardingReadinessStepType, dex.GetFinalStepType[bamboohr.GetEmployeeResult](checkOnboardingReadiness{}))
	require.Equal(t, decideProvisioningHandoffStepType, dex.GetFinalStepType[bamboohr.ListTimeOffRequestsResult](decideProvisioningHandoff{}))
	require.Equal(t, completeOnboardingCheckStepType, dex.GetFinalStepType[bamboohr.UpdateEmployeeResult](completeOnboardingCheck{}))
	wait, err := recordNewHire{}.WaitFor(nil, Input{})
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

func TestBuildNewHireValidatesStartInput(t *testing.T) {
	hire, err := BuildNewHire(Input{
		FirstName: " Ava ", LastName: "Nguyen", PersonalEmail: " ava.nguyen@personal.example.com ", HireDate: "2026-10-13",
		HandoffFieldName: "customITProvisioning",
	})
	require.NoError(t, err)
	require.Equal(t, NewHire{
		FirstName: "Ava", LastName: "Nguyen", PersonalEmail: "ava.nguyen@personal.example.com", HireDate: "2026-10-13", HandoffFieldName: "customITProvisioning",
	}, hire)
	valid := Input{FirstName: "Ava", LastName: "Nguyen", PersonalEmail: "ava@example.com", HireDate: "2026-10-13", HandoffFieldName: "customIT"}
	for name, change := range map[string]func(*Input){
		"no first name":        func(input *Input) { input.FirstName = " " },
		"display address":      func(input *Input) { input.PersonalEmail = "Ava <ava@example.com>" },
		"us date":              func(input *Input) { input.HireDate = "10/13/2026" },
		"standard field":       func(input *Input) { input.HandoffFieldName = "workEmail" },
		"history field":        func(input *Input) { input.HandoffFieldName = "jobTitle" },
		"numeric field id":     func(input *Input) { input.HandoffFieldName = "4047" },
		"blank handoff field":  func(input *Input) { input.HandoffFieldName = "" },
		"impossible hire date": func(input *Input) { input.HireDate = "2026-02-30" },
	} {
		input := valid
		change(&input)
		_, err := BuildNewHire(input)
		require.Error(t, err, name)
	}
}

func TestMappersPassOnlyTheRecordedHire(t *testing.T) {
	hire := NewHire{FirstName: "Ava", LastName: "Nguyen", PersonalEmail: "ava@example.com", HireDate: "2026-10-13", HandoffFieldName: "customIT"}
	require.Equal(t, bamboohr.FindEmployeeByEmailInput{Email: "ava@example.com", EmailField: bamboohr.EmployeeEmailFieldHome}, MapToFindEmployeeByEmailInput(hire))
	require.Equal(t, bamboohr.AddEmployeeInput{FirstName: "Ava", LastName: "Nguyen", HomeEmail: "ava@example.com", HireDate: "2026-10-13"}, MapToAddEmployeeInput(hire))
	require.Equal(t, bamboohr.GetEmployeeInput{EmployeeID: "140", Fields: []string{
		"firstName", "lastName", "homeEmail", "workEmail", "status", "department", "jobTitle", "location", "supervisorEId", "hireDate", "customIT",
	}}, MapToGetEmployeeInput(EmployeeRecordRequest{EmployeeID: "140", HandoffFieldName: "customIT"}))
	require.Equal(t, bamboohr.ListTimeOffRequestsInput{
		StartDate: "2026-10-13", EndDate: "2026-10-26", EmployeeID: "140", PageSize: timeOffPageSize,
		Statuses: []bamboohr.TimeOffRequestStatus{bamboohr.TimeOffRequestStatusApproved, bamboohr.TimeOffRequestStatusRequested},
	}, MapToListTimeOffRequestsInput(StartWindow{EmployeeID: "140", StartDate: "2026-10-13", EndDate: "2026-10-26"}))
	require.Equal(t, bamboohr.UpdateEmployeeInput{EmployeeID: "140", Fields: map[string]string{"customIT": "Requested"}},
		MapToUpdateEmployeeInput(HandoffRecord{EmployeeID: "140", FieldName: "customIT", Value: "Requested"}))
}

func TestFindMissingProvisioningFieldsTreatsOmittedFieldsAndInactiveHiresAsMissing(t *testing.T) {
	complete := bamboohr.EmployeeRecord{EmployeeID: "140", Fields: map[string]string{
		"department": "Engineering", "jobTitle": "Platform Engineer", "location": "Salt Lake City", "supervisorEId": "101",
		"hireDate": "2026-10-13", "status": "Active",
	}}
	require.Empty(t, FindMissingProvisioningFields(complete))
	incomplete := bamboohr.EmployeeRecord{EmployeeID: "140", Fields: map[string]string{
		"department": " ", "jobTitle": "Platform Engineer", "hireDate": "2026-10-13", "status": "Inactive",
	}, OmittedFields: []string{"location", "supervisorEId"}}
	require.Equal(t, []string{"department", "location", "supervisorEId", "status"}, FindMissingProvisioningFields(incomplete))
}

func TestBuildStartWindowCoversTheFirstTwoWeeks(t *testing.T) {
	window, err := BuildStartWindow("140", "2026-12-28")
	require.NoError(t, err)
	require.Equal(t, StartWindow{EmployeeID: "140", StartDate: "2026-12-28", EndDate: "2027-01-10"}, window)
	_, err = BuildStartWindow("140", "")
	require.Error(t, err)
}

func TestBuildHandoffValueIsDeterministicAndBounded(t *testing.T) {
	details := map[string]string{
		"hireDate": "2026-10-13", "jobTitle": "Platform Engineer", "department": "Engineering", "location": "Salt Lake City", "supervisorEId": "101",
	}
	require.Equal(t, "IT provisioning requested by Dex for a 2026-10-13 start: Platform Engineer, Engineering, Salt Lake City; manager employee 101; first-two-weeks time off: none",
		BuildHandoffValue(details, nil))
	var timeOff []StartWindowTimeOff
	for index := 0; index < 7; index++ {
		timeOff = append(timeOff, StartWindowTimeOff{RequestID: int64(index), StartDate: "2026-10-14", EndDate: "2026-10-14", Status: bamboohr.TimeOffRequestStatusApproved})
	}
	value := BuildHandoffValue(details, timeOff)
	require.Equal(t, maxListedTimeOff, strings.Count(value, "(APPROVED)"))
	require.True(t, strings.HasSuffix(value, "and 2 more"))
	require.LessOrEqual(t, len(value), bamboohr.MaxFieldValueBytes)
}

func newUnitTestConnection(t *testing.T, connectionName string) bamboohr.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "bamboohr", Name: connectionName}
	client, err := bamboohr.New(bamboohr.Config{CompanyDomain: "acme"},
		sdkgo.StaticCredentialProvider[bamboohr.Credentials]{reference: {APIKey: sdkgo.NewSecretString("0123456789abcdef" + "0123456789abcdef01234567")}})
	require.NoError(t, err)
	connection, err := bamboohr.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}
