// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack_test

import (
	"encoding/json"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net"
	"net/http"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	if scenario.Name == "postChannelMessage-transport-before-send" || scenario.Name == "postThreadReply-transport-before-send" ||
		scenario.Name == "postButtonMessage-transport-before-send" {
		wrapped := *transport
		wrapped.Transport = beforeSendConformanceTransport{transport.Transport}
		transport = &wrapped
	}
	credentials := sdkgo.StaticCredentialProvider[slack.Credentials]{slackConnection: {
		BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"),
	}}
	if scenario.Name == "getWorkspaceIdentity-defect" {
		credentials[slackConnection] = slack.Credentials{UserToken: sdkgo.NewSecretString("user-token")}
	}
	client, err := slack.New(slack.Config{Endpoint: endpoint}, credentials, slack.WithHTTPClient(transport))
	if err != nil {
		t.Fatal(err)
	}
	context := newSlackDexContext("conformance")
	if len(scenario.PriorHeartbeat) != 0 {
		context.heartbeat = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		var marker struct {
			Attempt int32 `json:"attempt"`
		}
		if err := json.Unmarshal(scenario.PriorHeartbeat, &marker); err != nil {
			t.Fatal(err)
		}
		context.attempt = marker.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listChannels":         newConformanceQueryInvocation(t, scenario.Input, context, client.ListChannels(), slackConnection),
		"getWorkspaceIdentity": newConformanceQueryInvocation(t, scenario.Input, context, client.GetWorkspaceIdentity(), slackConnection),
		"listThreadMessages":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListThreadMessages(), slackConnection),
		"getThreadReply":       newConformanceQueryInvocation(t, scenario.Input, context, client.GetThreadReply(), slackConnection),
		"listChannelMessages":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListChannelMessages(), slackConnection),
		"postChannelMessage":   newConformanceMutationInvocation(t, scenario.Input, context, client.PostChannelMessage(), slackConnection, scenario.CallID),
		"postThreadReply":      newConformanceMutationInvocation(t, scenario.Input, context, client.PostThreadReply(), slackConnection, scenario.CallID),
		"postButtonMessage":    newConformanceMutationInvocation(t, scenario.Input, context, client.PostButtonMessage(), slackConnection, scenario.CallID),
	}}
}

type beforeSendConformanceTransport struct{ next http.RoundTripper }

func (transport beforeSendConformanceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.next.RoundTrip(request)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Err: err}
	}
	return response, nil
}
