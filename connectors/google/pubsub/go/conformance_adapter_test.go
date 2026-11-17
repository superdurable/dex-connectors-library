// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pubsub

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google-pubsub", Name: "google-pubsub"}
	credentials := Credentials{
		OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
		AccessToken: sdkgo.NewSecretString("test-google-pubsub-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"),
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &pubsubConformanceContext{}
	previousAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.previousHeartbeat = append(json.RawMessage(nil), value...) })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.previousHeartbeat = context.recordedHeartbeat
			context.recordedHeartbeat = nil
		}
		context.attemptNo = previousAttempt + int32(attempt)
	}, actions: map[string]conformanceOperationInvocation{
		"listTopics":               newConformanceQueryInvocation(t, scenario.Input, context, client.ListTopics(), reference),
		"getTopic":                 newConformanceQueryInvocation(t, scenario.Input, context, client.GetTopic(), reference),
		"createTopic":              newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTopic(), reference, scenario.CallID),
		"updateTopicLabels":        newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTopicLabels(), reference, scenario.CallID),
		"deleteTopic":              newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteTopic(), reference, scenario.CallID),
		"listSubscriptions":        newConformanceQueryInvocation(t, scenario.Input, context, client.ListSubscriptions(), reference),
		"getSubscription":          newConformanceQueryInvocation(t, scenario.Input, context, client.GetSubscription(), reference),
		"createSubscription":       newConformanceMutationInvocation(t, scenario.Input, context, client.CreateSubscription(), reference, scenario.CallID),
		"updateSubscriptionLabels": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateSubscriptionLabels(), reference, scenario.CallID),
		"deleteSubscription":       newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteSubscription(), reference, scenario.CallID),
		"publishMessage":           newConformanceMutationInvocation(t, scenario.Input, context, client.PublishMessage(), reference, scenario.CallID),
		"pullMessages":             newConformanceQueryInvocation(t, scenario.Input, context, client.PullMessages(), reference),
		"acknowledgeMessages":      newConformanceMutationInvocation(t, scenario.Input, context, client.AcknowledgeMessages(), reference, scenario.CallID),
	}}
}

type pubsubConformanceContext struct {
	queryContext
	attemptNo         int32
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
}

func (context *pubsubConformanceContext) Attempt() int32 { return context.attemptNo }
func (context *pubsubConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recordedHeartbeat = encoded
	return nil
}
func (context *pubsubConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.previousHeartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, value)
}
