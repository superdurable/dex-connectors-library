// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const conformanceEndpointPath = "/youtube/v3"
const conformanceFixturePathPrefix = ""

var conformanceConnection = sdkgo.ConnectionRef{Provider: "google", Name: "youtube"}

type conformanceContext struct{ dex.Context }

func (conformanceContext) FlowID() string              { return "youtube-conformance" }
func (conformanceContext) StepExecutionID() string     { return "youtube-step" }
func (conformanceContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (conformanceContext) Done() <-chan struct{}       { return nil }
func (conformanceContext) Err() error                  { return nil }
func (conformanceContext) Value(key any) any           { return context.Background().Value(key) }

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := New(Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		conformanceConnection: {AuthMethodID: ManageAuthMethodID, AccessToken: sdkgo.NewSecretString("one-use-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchVideos":      newConformanceQueryInvocation(t, scenario.Input, conformanceContext{}, client.SearchVideos(), conformanceConnection),
		"getVideo":          newConformanceQueryInvocation(t, scenario.Input, conformanceContext{}, client.GetVideo(), conformanceConnection),
		"getChannel":        newConformanceQueryInvocation(t, scenario.Input, conformanceContext{}, client.GetChannel(), conformanceConnection),
		"listPlaylistItems": newConformanceQueryInvocation(t, scenario.Input, conformanceContext{}, client.ListPlaylistItems(), conformanceConnection),
		"addPlaylistItem":   newConformanceMutationInvocation(t, scenario.Input, conformanceContext{}, client.AddPlaylistItem(), conformanceConnection, scenario.CallID),
	}}
}
