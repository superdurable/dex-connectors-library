// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := typeform.New(typeform.Config{}, sdkgo.StaticCredentialProvider[typeform.Credentials]{
		testConnection: {AuthMethodID: typeform.PersonalAccessTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(sentinelToken), WebhookSecret: sdkgo.NewSecretString("SENTINEL-TYPEFORM-WEBHOOK-SECRET")},
	}, typeform.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newStepContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listForms":     newConformanceQueryInvocation(t, scenario.Input, context, client.ListForms(), testConnection),
		"getForm":       newConformanceQueryInvocation(t, scenario.Input, context, client.GetForm(), testConnection),
		"listResponses": newConformanceQueryInvocation(t, scenario.Input, context, client.ListResponses(), testConnection),
		"upsertWebhook": newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertWebhook(), testConnection, scenario.CallID),
	}}
}
