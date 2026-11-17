// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

var conformanceConnection = sdkgo.ConnectionRef{Provider: "amazon-sqs", Name: "sqs-test"}
var conformanceClock = time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{conformanceConnection: {
		AccessKeyID:     sdkgo.NewSecretString("AKIA" + "SQSCONNECTORTEST"),
		SecretAccessKey: sdkgo.NewSecretString("sqs-connector-test/secret+value"),
	}}
	client, err := New(Config{Endpoint: endpoint, Region: "eu-west-1", DefaultQueueURL: "http://127.0.0.1:12345/123456789012/orders"}, credentials, WithHTTPClient(transport))
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return conformanceClock }
	context := &conformanceDexContext{Context: context.Background(), step: "conformance"}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(checkpoint []byte) { context.heartbeat = checkpoint })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = priorAttempt + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"sendMessage":             newConformanceMutationInvocation(t, scenario.Input, context, client.SendMessage(), conformanceConnection, scenario.CallID),
		"receiveMessages":         newConformanceMutationInvocation(t, scenario.Input, context, client.ReceiveMessages(), conformanceConnection, scenario.CallID),
		"deleteMessage":           newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteMessage(), conformanceConnection, scenario.CallID),
		"changeMessageVisibility": newConformanceMutationInvocation(t, scenario.Input, context, client.ChangeMessageVisibility(), conformanceConnection, scenario.CallID),
		"listQueues":              newConformanceQueryInvocation(t, scenario.Input, context, client.ListQueues(), conformanceConnection),
		"createQueue":             newConformanceMutationInvocation(t, scenario.Input, context, client.CreateQueue(), conformanceConnection, scenario.CallID),
		"getQueueURL":             newConformanceQueryInvocation(t, scenario.Input, context, client.GetQueueURL(), conformanceConnection),
		"getQueueAttributes":      newConformanceQueryInvocation(t, scenario.Input, context, client.GetQueueAttributes(), conformanceConnection),
		"setQueueAttribute":       newConformanceMutationInvocation(t, scenario.Input, context, client.SetQueueAttribute(), conformanceConnection, scenario.CallID),
		"deleteQueue":             newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteQueue(), conformanceConnection, scenario.CallID),
	}}
}

type conformanceDexContext struct {
	context.Context
	step      string
	attempt   int32
	heartbeat json.RawMessage
}

func (*conformanceDexContext) FlowID() string                        { return "registration-123" }
func (*conformanceDexContext) RunID() string                         { return "run" }
func (*conformanceDexContext) FlowStartedAt() time.Time              { return time.Unix(1, 0) }
func (c *conformanceDexContext) StepExecutionID() string             { return c.step }
func (*conformanceDexContext) FromStepExecutionID() string           { return "" }
func (*conformanceDexContext) RecoveryError() *dex.RecoveryErrorInfo { return nil }
func (*conformanceDexContext) FirstAttemptAt() time.Time             { return time.Unix(1, 0) }
func (c *conformanceDexContext) Attempt() int32                      { return c.attempt }
func (*conformanceDexContext) HasTimerFired() bool                   { return false }
func (*conformanceDexContext) HasTimerFiredByIndex(int) bool         { return false }
func (*conformanceDexContext) WaitForMethodFailed() bool             { return false }
func (c *conformanceDexContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		c.heartbeat = encoded
	}
	return err
}
func (c *conformanceDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(c.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(c.heartbeat, target)
}
func (*conformanceDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*conformanceDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*conformanceDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*conformanceDexContext)(nil)
