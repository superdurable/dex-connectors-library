// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package httpconnector_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryTarget struct {
	dex.StepDefaultsNoWaitFor[httpconnector.QueryStepOutput[string]]
}

func (queryTarget) Execute(dex.Context, httpconnector.QueryStepOutput[string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type mutationTarget struct {
	dex.StepDefaultsNoWaitFor[httpconnector.MutationStepOutput[string]]
}

func (mutationTarget) Execute(dex.Context, httpconnector.MutationStepOutput[string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type webhookTarget struct {
	dex.StepDefaultsNoWaitFor[httpconnector.VerifyWebhookStepOutput[string]]
}

func (webhookTarget) Execute(dex.Context, httpconnector.VerifyWebhookStepOutput[string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestOperationSpecificFactoriesUseTypedConnectionAndBranches(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "http", Name: "factory-test"}
	client, err := httpconnector.New(httpconnector.Config{BaseURL: "https://example.com"}, sdkgo.StaticCredentialProvider[httpconnector.Credentials]{reference: {}})
	require.NoError(t, err)
	connection, err := httpconnector.NewConnection(client, reference)
	require.NoError(t, err)

	query := httpconnector.NewQueryStep(httpconnector.QueryStepConfig[string]{
		StepType: "HTTPQuery", Presentation: factoryPresentation(), Connection: connection,
		BuildInput: func(string) (httpconnector.Request, error) { return httpconnector.Request{Method: http.MethodGet}, nil },
		Succeeded:  sdkgo.GoTo(queryTarget{}), Failed: sdkgo.GoTo(queryTarget{}), Defect: sdkgo.GoTo(queryTarget{}),
	})
	require.Equal(t, "HTTPQuery", query.GetStepType())

	attribute := dex.DefineAttribute[sdkgo.MutationResult[httpconnector.Response]]("http-factory-result")
	mutation := httpconnector.NewMutationStep(httpconnector.MutationStepConfig[string]{
		StepType: "HTTPMutation", Presentation: factoryPresentation(), Connection: connection,
		BuildInput: func(string) (httpconnector.Request, error) {
			return httpconnector.Request{Method: http.MethodPost}, nil
		},
		Succeeded: sdkgo.GoTo(mutationTarget{}), Rejected: sdkgo.GoTo(mutationTarget{}),
		Uncertain: sdkgo.GoTo(mutationTarget{}), Defect: sdkgo.GoTo(mutationTarget{}), ResultAttribute: &attribute,
	})
	require.Equal(t, "HTTPMutation", mutation.GetStepType())

	webhook := httpconnector.NewVerifyWebhookStep(httpconnector.VerifyWebhookStepConfig[string]{
		StepType: "VerifyWebhook", Presentation: factoryPresentation(), Connection: connection,
		BuildInput: func(string) (httpconnector.WebhookRequest, error) { return httpconnector.WebhookRequest{}, nil },
		Verified:   sdkgo.GoTo(webhookTarget{}), Rejected: sdkgo.GoTo(webhookTarget{}), Defect: sdkgo.GoTo(webhookTarget{}),
	})
	require.Equal(t, "VerifyWebhook", webhook.GetStepType())
}

func TestTypedConnectionCannotSerializeAndZeroValueFailsClosed(t *testing.T) {
	encoded, err := json.Marshal(httpconnector.Connection{})
	require.ErrorContains(t, err, "cannot be serialized")
	require.Nil(t, encoded)
	require.NotContains(t, fmt.Sprintf("%#v", httpconnector.Connection{}), "client")
	require.Panics(t, func() {
		httpconnector.NewQueryStep(httpconnector.QueryStepConfig[string]{Connection: httpconnector.Connection{}})
	})
}

func factoryPresentation() sdkgo.StepPresentation {
	return sdkgo.StepPresentation{GroupID: "http", GroupLabel: "HTTP", Explanation: "Invoke the HTTP sdkgo."}
}
