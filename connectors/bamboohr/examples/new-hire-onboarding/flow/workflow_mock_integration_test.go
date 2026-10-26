//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newhireonboarding

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr/bamboohrmock"
)

func TestNewHireOnboardingUsesMockWithRealDex(t *testing.T) {
	mock := bamboohrmock.New(t, ConnectionName)
	mock.FindEmployeeByEmail().Respond(bamboohrmock.FindEmployeeByEmailNotFoundFailure())
	mock.GetEmployee().Respond(bamboohrmock.GetEmployeeFound(bamboohr.EmployeeRecord{
		EmployeeID: "142",
		Fields: map[string]string{
			"firstName": "Sam", "lastName": "Park", "homeEmail": "sam.park@personal.example.com",
			"hireDate": "2026-10-13", "status": "Active", "customITProvisioning": "",
		},
	}, nil))
	harness := newOnboardingHarnessForConnection(t, mock.Connection())
	outcome := harness.runOnboarding(t, "mock-new-hire", Input{
		FirstName: " Sam ", LastName: "Park", PersonalEmail: "sam.park@personal.example.com",
		HireDate: "2026-10-13", HandoffFieldName: "customITProvisioning",
	})
	require.Equal(t, OnboardingHeld, outcome.Action)
	require.True(t, outcome.WasEmployeeAdded)
	require.False(t, outcome.WasAdditionReconciled)
	require.Equal(t, []string{"department", "jobTitle", "location", "supervisorEId"}, outcome.MissingFields)

	findCalls := mock.FindEmployeeByEmail().Calls()
	require.Len(t, findCalls, 1)
	require.Equal(t, bamboohr.FindEmployeeByEmailInput{Email: "sam.park@personal.example.com", EmailField: bamboohr.EmployeeEmailFieldHome}, findCalls[0].Input)
	require.Equal(t, bamboohr.FindEmployeeByEmailBranchNotFound, findCalls[0].Branch)
	addCalls := mock.AddEmployee().Calls()
	require.Len(t, addCalls, 1)
	require.Equal(t, bamboohr.AddEmployeeInput{FirstName: "Sam", LastName: "Park", HomeEmail: "sam.park@personal.example.com", HireDate: "2026-10-13"}, addCalls[0].Input)
	require.Equal(t, bamboohr.AddEmployeeBranchCreated, addCalls[0].Branch)
	getCalls := mock.GetEmployee().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, bamboohr.GetEmployeeInput{EmployeeID: "142", Fields: []string{
		"firstName", "lastName", "homeEmail", "workEmail", "status", "department", "jobTitle", "location", "supervisorEId", "hireDate", "customITProvisioning",
	}}, getCalls[0].Input)
	require.Empty(t, mock.ListTimeOffRequests().Calls())
	require.Empty(t, mock.UpdateEmployee().Calls())
}
