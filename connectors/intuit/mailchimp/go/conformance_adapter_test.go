// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go"
)

const conformanceEndpointPath = "/3.0"
const conformanceFixturePathPrefix = "/3.0"

type mailchimpConformanceTransport struct {
	recorder *conformanceRecorder
}

func (transport mailchimpConformanceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	index := transport.recorder.requestCount
	response, err := transport.recorder.RoundTrip(request)
	if err != nil && index < len(transport.recorder.scenario.Exchanges) && transport.recorder.scenario.Exchanges[index].TransportFailure == "before-send" {
		return nil, &net.OpError{Op: "dial", Err: err}
	}
	return response, err
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	transport.Transport = mailchimpConformanceTransport{recorder: transport.Transport.(*conformanceRecorder)}
	client, err := mailchimp.New(mailchimp.Config{}, testCredentialProvider("TESTKEY1-us6"),
		mailchimp.WithAPIBaseURL(endpoint), mailchimp.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newMailchimpDexContext("conformance")
	initialAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.previousHeartbeat = raw }) + 1
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.previousHeartbeat = context.recordedHeartbeat
			context.recordedHeartbeat = nil
		}
		context.attempt = initialAttempt + int32(attempt-1)
	}, actions: map[string]conformanceOperationInvocation{
		"getMember":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetMember(), mailchimpConnection),
		"listMembers":      newConformanceQueryInvocation(t, scenario.Input, context, client.ListMembers(), mailchimpConnection),
		"upsertMember":     newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertMember(), mailchimpConnection, scenario.CallID),
		"updateMemberTags": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateMemberTags(), mailchimpConnection, scenario.CallID),
		"sendCampaign":     newConformanceMutationInvocation(t, scenario.Input, context, client.SendCampaign(), mailchimpConnection, scenario.CallID),
	}}
}
