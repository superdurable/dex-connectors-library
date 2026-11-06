// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath, conformanceFixturePathPrefix = "", "/v1.0"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	credentials := delegatedCredentials()
	credentials.AccessToken = sdkgo.NewSecretString("OUTLOOK_CONFORMANCE_TOKEN")
	config := outlookmail.Config{}
	if strings.Contains(scenario.Name, "appOnly") {
		credentials.AuthMethodID = outlookmail.AppOnlyAuthMethodID
		credentials.TenantID = testTenantID
		config.Mailbox = testMailbox
	}
	client, err := outlookmail.New(config, sdkgo.StaticCredentialProvider[outlookmail.Credentials]{
		outlookConnection: credentials,
	}, outlookmail.WithHTTPClient(transport), outlookmail.WithLocalProviderURL(endpoint), outlookmail.WithClock(conformanceFixedClock(t, scenario)))
	require.NoError(t, err)
	context := newOutlookDexContext("conformance")
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.previousHeartbeat = raw })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 && context.recordedHeartbeat != nil {
			context.previousHeartbeat = context.recordedHeartbeat
		}
		context.attempt = prior + int32(attempt)
	},
		actions: map[string]conformanceOperationInvocation{
			"searchMessages":  newConformanceQueryInvocation(t, scenario.Input, context, client.SearchMessages(), outlookConnection),
			"getMessage":      newConformanceQueryInvocation(t, scenario.Input, context, client.GetMessage(), outlookConnection),
			"sendMessage":     newConformanceMutationInvocation(t, scenario.Input, context, client.SendMessage(), outlookConnection, scenario.CallID),
			"replyToMessage":  newConformanceMutationInvocation(t, scenario.Input, context, client.ReplyToMessage(), outlookConnection, scenario.CallID),
			"moveMessage":     newConformanceMutationInvocation(t, scenario.Input, context, client.MoveMessage(), outlookConnection, scenario.CallID),
			"setMessageFlags": newConformanceMutationInvocation(t, scenario.Input, context, client.SetMessageFlags(), outlookConnection, scenario.CallID),
		}}
}
