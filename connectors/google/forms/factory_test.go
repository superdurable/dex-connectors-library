// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package forms_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/forms"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type listTarget struct {
	dex.StepDefaultsNoWaitFor[forms.ListResponsesResult]
}

func (listTarget) Execute(dex.Context, forms.ListResponsesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type formTarget struct {
	dex.StepDefaultsNoWaitFor[forms.GetFormResult]
}

func (formTarget) Execute(dex.Context, forms.GetFormResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

var testAnnotations = sdkgo.StepAnnotations{GroupID: "google", GroupLabel: "Google", Explanation: "Use Google Forms."}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection, err := forms.NewConnection(newFormsClient(t, "http://127.0.0.1:1"), formsConnection)
	require.NoError(t, err)
	require.NotPanics(t, func() {
		forms.NewListResponsesStep(forms.ListResponsesStepConfig[string]{
			StepType: "List", Annotations: testAnnotations, Connection: connection, ConnectionName: formsConnection.Name,
			MapToOperationInput: func(string) forms.ListResponsesInput { return forms.ListResponsesInput{} },
			Listed:              sdkgo.GoTo(listTarget{}),
		})
		forms.NewGetFormStep(forms.GetFormStepConfig[string]{
			StepType: "Read", Annotations: testAnnotations, Connection: connection,
			MapToOperationInput: func(string) forms.GetFormInput { return forms.GetFormInput{} },
			Found:               sdkgo.GoTo(formTarget{}), NotFound: sdkgo.GoTo(formTarget{}),
		})
	})
	require.Panics(t, func() {
		forms.NewGetFormStep(forms.GetFormStepConfig[string]{
			StepType: "Read", Annotations: testAnnotations, Connection: connection,
			MapToOperationInput: func(string) forms.GetFormInput { return forms.GetFormInput{} },
			NotFound:            sdkgo.GoTo(formTarget{}),
		})
	})
}

func TestFactoriesRejectAConnectionNameMismatch(t *testing.T) {
	connection, err := forms.NewConnection(newFormsClient(t, "http://127.0.0.1:1"), formsConnection)
	require.NoError(t, err)
	require.Panics(t, func() {
		forms.NewListResponsesStep(forms.ListResponsesStepConfig[string]{
			StepType: "List", Annotations: testAnnotations, Connection: connection, ConnectionName: "different",
			MapToOperationInput: func(string) forms.ListResponsesInput { return forms.ListResponsesInput{} },
			Listed:              sdkgo.GoTo(listTarget{}),
		})
	})
}

func TestFormsConnectionCannotBeSerialized(t *testing.T) {
	connection, err := forms.NewConnection(newFormsClient(t, "http://127.0.0.1:1"), formsConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), formsTestToken)
	require.Equal(t, "forms.Connection{[REDACTED]}", connection.String())
}
