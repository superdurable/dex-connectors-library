// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath, conformanceFixturePathPrefix = "/gmail/v1", "/gmail/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := gmail.New(gmail.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[gmail.Credentials]{
		gmailConnection: {AccessToken: sdkgo.NewSecretString("gmail-token"), PrimaryEmail: "owner@example.com"},
	}, gmail.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newGmailDexContext("conformance")
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.lastHeartbeat = raw })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = prior + int32(attempt) },
		actions: map[string]conformanceOperationInvocation{
			"getMessage":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetMessage(), gmailConnection),
			"sendMessage":    newConformanceMutationInvocation(t, scenario.Input, context, client.SendMessage(), gmailConnection, scenario.CallID),
			"replyToMessage": newConformanceMutationInvocation(t, scenario.Input, context, client.ReplyToMessage(), gmailConnection, scenario.CallID),
		}}
}
