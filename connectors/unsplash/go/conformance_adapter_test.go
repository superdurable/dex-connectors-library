// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package unsplash

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	connection := sdkgo.ConnectionRef{Provider: "unsplash", Name: "unsplash"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{connection: {AccessKey: sdkgo.NewSecretString("unsplash-test-key")}}, conformanceHTTPClientOption{transport})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
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
	}}
}

type conformanceHTTPClientOption struct{ client *http.Client }

func (option conformanceHTTPClientOption) applyClientOption(client *Client) {
	client.httpClient = option.client
}
