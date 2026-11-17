// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

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
	config := Config{BaseURL: endpoint, UploadBaseURL: endpoint, MaxDownloadBytes: 8, MaxUploadBytes: 8}
	if scenario.Name == "downloadFile-invalidResponse" {
		config.MaxResponseBytes = 8
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: testCredentials()}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"downloadFile":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.DownloadFile(), testConnection),
		"uploadFile":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UploadFile(), testConnection, scenario.CallID),
		"getFile":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetFile(), testConnection),
		"getFolder":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetFolder(), testConnection),
		"listFolderItems": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListFolderItems(), testConnection),
		"searchFiles":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchFiles(), testConnection),
		"searchFolders":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchFolders(), testConnection),
		"deleteFile":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteFile(), testConnection, scenario.CallID),
		"deleteFolder":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteFolder(), testConnection, scenario.CallID),
	}}
}
