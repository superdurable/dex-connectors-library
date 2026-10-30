// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type completeTarget[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (completeTarget[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

// GetStepType names the target: Dex gives a generic Step type no default name.
func (completeTarget[IN]) GetStepType() string { return "Complete" }

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "bamboohr", GroupLabel: "BambooHR", Explanation: "Call BambooHR."}
	require.NotPanics(t, func() {
		bamboohr.NewGetEmployeeStep(bamboohr.GetEmployeeStepConfig[string]{
			StepType: "ReadEmployee", Annotations: annotations, Connection: connection, ConnectionName: bambooHRConnection.Name,
			MapToOperationInput: func(id string) bamboohr.GetEmployeeInput {
				return bamboohr.GetEmployeeInput{EmployeeID: id, Fields: []string{"firstName"}}
			},
			Found: sdkgo.GoTo(completeTarget[bamboohr.GetEmployeeResult]{}),
		})
		bamboohr.NewFindEmployeeByEmailStep(bamboohr.FindEmployeeByEmailStepConfig[string]{
			StepType: "FindEmployee", Annotations: annotations, Connection: connection, ConnectionName: bambooHRConnection.Name,
			MapToOperationInput: func(email string) bamboohr.FindEmployeeByEmailInput {
				return bamboohr.FindEmployeeByEmailInput{Email: email}
			},
			Found: sdkgo.GoTo(completeTarget[bamboohr.FindEmployeeByEmailResult]{}),
		})
		bamboohr.NewListEmployeeChangesStep(bamboohr.ListEmployeeChangesStepConfig[time.Time]{
			StepType: "ListChanges", Annotations: annotations, Connection: connection, ConnectionName: bambooHRConnection.Name,
			MapToOperationInput: func(since time.Time) bamboohr.ListEmployeeChangesInput {
				return bamboohr.ListEmployeeChangesInput{Since: since}
			},
			Listed: sdkgo.GoTo(completeTarget[bamboohr.ListEmployeeChangesResult]{}),
		})
		bamboohr.NewListTimeOffRequestsStep(bamboohr.ListTimeOffRequestsStepConfig[string]{
			StepType: "ListTimeOff", Annotations: annotations, Connection: connection, ConnectionName: bambooHRConnection.Name,
			MapToOperationInput: func(day string) bamboohr.ListTimeOffRequestsInput {
				return bamboohr.ListTimeOffRequestsInput{StartDate: day, EndDate: day}
			},
			Listed: sdkgo.GoTo(completeTarget[bamboohr.ListTimeOffRequestsResult]{}),
		})
		bamboohr.NewUpdateEmployeeStep(bamboohr.UpdateEmployeeStepConfig[string]{
			StepType: "UpdateEmployee", Annotations: annotations, Connection: connection, ConnectionName: bambooHRConnection.Name,
			MapToOperationInput: func(id string) bamboohr.UpdateEmployeeInput {
				return bamboohr.UpdateEmployeeInput{EmployeeID: id, Fields: map[string]string{"customITProvisioning": "Requested"}}
			},
			Updated: sdkgo.GoTo(completeTarget[bamboohr.UpdateEmployeeResult]{}),
		})
		bamboohr.NewAddEmployeeStep(bamboohr.AddEmployeeStepConfig[string]{
			StepType: "AddEmployee", Annotations: annotations, Connection: connection, ConnectionName: bambooHRConnection.Name,
			MapToOperationInput: func(string) bamboohr.AddEmployeeInput { return validAddEmployeeInput() },
			Created:             sdkgo.GoTo(completeTarget[bamboohr.AddEmployeeResult]{}),
			Uncertain:           sdkgo.GoTo(completeTarget[bamboohr.AddEmployeeResult]{}),
		})
	})
	require.Panics(t, func() {
		bamboohr.NewAddEmployeeStep(bamboohr.AddEmployeeStepConfig[string]{
			StepType: "AddEmployee", Annotations: annotations, Connection: connection, ConnectionName: bambooHRConnection.Name,
			MapToOperationInput: func(string) bamboohr.AddEmployeeInput { return validAddEmployeeInput() },
			Uncertain:           sdkgo.GoTo(completeTarget[bamboohr.AddEmployeeResult]{}),
		})
	}, "created is the required branch")
	require.Panics(t, func() {
		bamboohr.NewGetEmployeeStep(bamboohr.GetEmployeeStepConfig[string]{
			StepType: "ReadEmployee", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(id string) bamboohr.GetEmployeeInput { return bamboohr.GetEmployeeInput{EmployeeID: id} },
			Found:               sdkgo.GoTo(completeTarget[bamboohr.GetEmployeeResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(bamboohr.GetEmployeeDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "ambiguous": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(bamboohr.FindEmployeeByEmailDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"listed": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(bamboohr.ListEmployeeChangesDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"listed": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(bamboohr.ListTimeOffRequestsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"updated": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(bamboohr.UpdateEmployeeDefinition.Branches), "an absolute plain-field update is safe to repeat, so it has no uncertain branch")
	require.Equal(t, map[sdkgo.BranchID]bool{"created": false, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(bamboohr.AddEmployeeDefinition.Branches), "BambooHR has no idempotency key, so an unconfirmed add is uncertain")
	for _, defaults := range []sdkgo.StepDefaults{
		bamboohr.GetEmployeeDefinition.StepDefaults, bamboohr.FindEmployeeByEmailDefinition.StepDefaults, bamboohr.ListEmployeeChangesDefinition.StepDefaults,
		bamboohr.ListTimeOffRequestsDefinition.StepDefaults, bamboohr.UpdateEmployeeDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "a duplicate dispatch of a read or an absolute update is harmless")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, "three 9-second requests fit inside it")
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute, "a one-minute Retry-After fits in the window")
	}
	add := bamboohr.AddEmployeeDefinition.StepDefaults
	require.Equal(t, dex.StepDurabilitySync, add.ExecuteDurability, "async would dispatch a second add after seven seconds")
	require.Equal(t, 30*time.Second, add.ExecuteMethodTimeout)
	require.GreaterOrEqual(t, add.ExecuteRetry.TotalDuration, 2*time.Minute)
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAPIKey)
	credentials := bamboohr.Credentials{APIKey: sdkgo.NewSecretString(testAPIKey)}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAPIKey)
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func TestBambooHREnumsKeepBambooHRsValues(t *testing.T) {
	require.Equal(t, []bamboohr.TimeOffRequestStatus{"REQUESTED", "APPROVED", "DENIED", "CANCELED"}, bamboohr.TimeOffRequestStatuses())
	require.Equal(t, bamboohr.EmployeeChangeAction("Inserted"), bamboohr.EmployeeChangeActionInserted)
	require.Equal(t, bamboohr.EmployeeChangeType("inserted"), bamboohr.EmployeeChangeTypeInserted)
	require.Equal(t, bamboohr.EmployeeStatusFilter("active"), bamboohr.EmployeeStatusFilterActive)
	require.Equal(t, bamboohr.EmployeeEmailField("homeEmail"), bamboohr.EmployeeEmailFieldHome)
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func newTestConnection(t *testing.T) bamboohr.Connection {
	t.Helper()
	client, err := bamboohr.New(bamboohr.Config{CompanyDomain: testCompanyDomain}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := bamboohr.NewConnection(client, bambooHRConnection)
	require.NoError(t, err)
	return connection
}
