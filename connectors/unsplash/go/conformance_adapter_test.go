// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package unsplash

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type downloadConformanceContext struct {
	providerContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *downloadConformanceContext) Attempt() int32 { return context.attempt }
func (context *downloadConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *downloadConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	connection := sdkgo.ConnectionRef{Provider: "unsplash", Name: "unsplash"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{connection: {AccessKey: sdkgo.NewSecretString("unsplash-test-key")}}, conformanceHTTPClientOption{transport})
	require.NoError(t, err)
	context := &downloadConformanceContext{attempt: 1, heartbeat: append(json.RawMessage(nil), scenario.PriorHeartbeat...)}
	priorAttempt := int32(0)
	if len(scenario.PriorHeartbeat) > 0 {
		var previous struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &previous))
		priorAttempt = previous.Attempt
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = int32(attempt+1) + priorAttempt }, actions: map[string]conformanceOperationInvocation{
		"searchPhotos":         newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.SearchPhotos(), connection),
		"getPhoto":             newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.GetPhoto(), connection),
		"listPhotos":           newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListPhotos(), connection),
		"getRandomPhoto":       newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.GetRandomPhoto(), connection),
		"searchCollections":    newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.SearchCollections(), connection),
		"listCollections":      newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListCollections(), connection),
		"getCollection":        newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.GetCollection(), connection),
		"listCollectionPhotos": newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListCollectionPhotos(), connection),
		"getUser":              newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.GetUser(), connection),
		"listUserPhotos":       newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListUserPhotos(), connection),
		"searchUsers":          newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.SearchUsers(), connection),
		"listTopics":           newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListTopics(), connection),
		"getTopic":             newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.GetTopic(), connection),
		"listTopicPhotos":      newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListTopicPhotos(), connection),
		"trackPhotoDownload":   newConformanceMutationInvocation(t, scenario.Input, context, client.TrackPhotoDownload(), connection, scenario.CallID),
	}}
}

type conformanceHTTPClientOption struct{ client *http.Client }

func (option conformanceHTTPClientOption) applyClientOption(client *Client) {
	client.httpClient = option.client
}
