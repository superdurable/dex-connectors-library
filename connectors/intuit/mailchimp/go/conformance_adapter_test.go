// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
	"encoding/json"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	mailchimp "github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go"
)

const conformanceEndpointPath, conformanceFixturePathPrefix = "/3.0", "/3.0"
const conformanceAPIKey = "mailchimpfixturekey-us6"

// A before-send event represents a dial refusal, which Go classifies by net.OpError.
type conformanceDialTransport struct {
	next http.RoundTripper
	recorder *conformanceRecorder
}

func (transport conformanceDialTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	index := transport.recorder.requestCount
	response, err := transport.next.RoundTrip(request)
	if err != nil && index < len(transport.recorder.scenario.Exchanges) &&
		transport.recorder.scenario.Exchanges[index].TransportFailure == "before-send" {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: err}
	}
	return response, err
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	transport.Transport = conformanceDialTransport{next: transport.Transport, recorder: transport.Transport.(*conformanceRecorder)}
	client, err := mailchimp.New(mailchimp.Config{}, testCredentialProvider(conformanceAPIKey),
		mailchimp.WithHTTPClient(transport), mailchimp.WithAPIBaseURL(endpoint))
	require.NoError(t, err)
	context := newMailchimpDexContext("conformance")
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) {
		if scenario.Operation == "sendCampaign" {
			marker, err := json.Marshal(map[string]string{"mailchimpDispatchedCallId": scenario.CallID})
			require.NoError(t, err)
			context.previousHeartbeat = marker
			return
		}
		context.previousHeartbeat = raw
	})
	return &conformanceMappedInvoker{t: t, scenario: scenario,
		before: func(attempt int) {
			if attempt > 1 {
				context.previousHeartbeat = context.recordedHeartbeat
			}
			context.attempt = prior + int32(attempt)
		},
		actions: map[string]conformanceOperationInvocation{
			"getMember":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetMember(), mailchimpConnection),
			"listMembers":      newConformanceQueryInvocation(t, scenario.Input, context, client.ListMembers(), mailchimpConnection),
			"upsertMember":     newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertMember(), mailchimpConnection, scenario.CallID),
			"updateMemberTags": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateMemberTags(), mailchimpConnection, scenario.CallID),
			"sendCampaign":     newConformanceMutationInvocation(t, scenario.Input, context, client.SendCampaign(), mailchimpConnection, scenario.CallID),
		},
	}
}
