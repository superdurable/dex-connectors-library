// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type conformanceRejectedCredentialSource struct{}

func (*conformanceRejectedCredentialSource) Resolve(sdkgo.Call) (Credentials, error) {
	return Credentials{AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

func (source *conformanceRejectedCredentialSource) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return source.Resolve(call)
}

func (*conformanceRejectedCredentialSource) ResolveAfterRejection(context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return Credentials{AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	connection := sdkgo.ConnectionRef{Provider: "github", Name: "github"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{
		connection: {AccessToken: sdkgo.NewSecretString("manifest-access-token")},
	}
	if scenario.Operation == "getAuthenticatedProfile" && strings.HasSuffix(scenario.Name, "-defect") {
		credentials[connection] = Credentials{}
	}
	var credentialSource CredentialSource = credentials
	if strings.HasSuffix(scenario.Name, "-refresh-after-rejection") {
		credentialSource = &conformanceRejectedCredentialSource{}
	}
	config := Config{BaseURL: endpoint}
	if strings.Contains(scenario.Name, "-oversized") {
		config.MaxResponseBytes = 100
	}
	client, err := New(config, credentialSource, WithHTTPClient(transport), WithClock(func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }))
	require.NoError(t, err)
	context := testsupport.NewDexContext("conformance", "github-step")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getAuthenticatedProfile": newConformanceQueryInvocation(t, scenario.Input, context, client.GetAuthenticatedProfile(), connection),
		"listPublicRepositories":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListPublicRepositories(), connection),
		"listMergedPullRequests":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListMergedPullRequests(), connection),
		"listPullRequestFiles":    newConformanceQueryInvocation(t, scenario.Input, context, client.ListPullRequestFiles(), connection),
		"listReleases":            newConformanceQueryInvocation(t, scenario.Input, context, client.ListReleases(), connection),
		"listCommits":             newConformanceQueryInvocation(t, scenario.Input, context, client.ListCommits(), connection),
	}}
}
