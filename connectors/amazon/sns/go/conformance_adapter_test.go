// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package sns

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
	"time"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "amazon-sns", Name: "amazon-sns"}
	credentials := Credentials{AccessKeyID: sdkgo.NewSecretString("AKIASNSCONNECTOR"), SecretAccessKey: sdkgo.NewSecretString("sns-test-secret"), SessionToken: sdkgo.NewSecretString("")}
	client, err := New(Config{Endpoint: endpoint, Region: "eu-west-1"}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport), signingTimeOption{now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }})
	require.NoError(t, err)
	publishContext := &publishTestContext{attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(checkpoint []byte) { publishContext.heartbeat = checkpoint })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { publishContext.attempt = priorAttempt + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"listTopics":                newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTopics(), reference),
		"createTopic":               newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateTopic(), reference, scenario.CallID),
		"deleteTopic":               newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteTopic(), reference, scenario.CallID),
		"setTopicAttribute":         newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SetTopicAttribute(), reference, scenario.CallID),
		"getTopicAttributes":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTopicAttributes(), reference),
		"publishMessage":            newConformanceMutationInvocation(t, scenario.Input, publishContext, client.PublishMessage(), reference, scenario.CallID),
		"subscribeEndpoint":         newConformanceMutationInvocation(t, scenario.Input, publishContext, client.SubscribeEndpoint(), reference, scenario.CallID),
		"listSubscriptionsByTopic":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSubscriptionsByTopic(), reference),
		"listSubscriptions":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSubscriptions(), reference),
		"getSubscriptionAttributes": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSubscriptionAttributes(), reference),
		"setSubscriptionAttribute":  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SetSubscriptionAttribute(), reference, scenario.CallID),
		"unsubscribeEndpoint":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UnsubscribeEndpoint(), reference, scenario.CallID),
	}}
}
