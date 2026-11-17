// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package whatsappbusiness

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/v25.0"
const conformanceFixturePathPrefix = "/v25.0"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "whatsapp-business", Name: "whatsapp-business"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: {AccessToken: sdkgo.NewSecretString("test-system-user-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	textContext := singledispatchtest.NewContext("whatsapp-business-conformance", "send-text")
	templateContext := singledispatchtest.NewContext("whatsapp-business-conformance", "send-template")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			textContext.AdvanceAttempt()
			templateContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listPhoneNumbers":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPhoneNumbers(), reference),
		"sendTextMessage":     newConformanceMutationInvocation(t, scenario.Input, textContext, client.SendTextMessage(), reference, scenario.CallID),
		"sendTemplateMessage": newConformanceMutationInvocation(t, scenario.Input, templateContext, client.SendTemplateMessage(), reference, scenario.CallID),
	}}
}
