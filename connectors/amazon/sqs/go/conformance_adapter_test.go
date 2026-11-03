// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs

import (
	"context"
	"errors"
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
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"receiveMessages":         newConformanceMutationInvocation(t, scenario.Input, context, client.ReceiveMessages(), conformanceConnection, scenario.CallID),
		"deleteMessage":           newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteMessage(), conformanceConnection, scenario.CallID),
		"changeMessageVisibility": newConformanceMutationInvocation(t, scenario.Input, context, client.ChangeMessageVisibility(), conformanceConnection, scenario.CallID),
	}}
}

type conformanceDexContext struct {
	context.Context
	step string
}

func (*conformanceDexContext) FlowID() string                        { return "registration-123" }
func (*conformanceDexContext) RunID() string                         { return "run" }
func (*conformanceDexContext) FlowStartedAt() time.Time              { return time.Unix(1, 0) }
func (c *conformanceDexContext) StepExecutionID() string             { return c.step }
func (*conformanceDexContext) FromStepExecutionID() string           { return "" }
func (*conformanceDexContext) RecoveryError() *dex.RecoveryErrorInfo { return nil }
func (*conformanceDexContext) FirstAttemptAt() time.Time             { return time.Unix(1, 0) }
func (*conformanceDexContext) Attempt() int32                        { return 1 }
func (*conformanceDexContext) HasTimerFired() bool                   { return false }
func (*conformanceDexContext) HasTimerFiredByIndex(int) bool         { return false }
func (*conformanceDexContext) WaitForMethodFailed() bool             { return false }
func (*conformanceDexContext) RecordHeartbeat(any) error             { return nil }
func (*conformanceDexContext) GetLastHeartbeatValue(any) (bool, error) {
	return false, errors.New("no heartbeat")
}
func (*conformanceDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*conformanceDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*conformanceDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*conformanceDexContext)(nil)
