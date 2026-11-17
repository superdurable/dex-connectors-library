// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

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
	config := Config{BaseURL: endpoint, UploadBaseURL: endpoint, MaxDownloadBytes: 8, MaxUploadBytes: 8}
	if scenario.Name == "downloadFile-invalidResponse" {
		config.MaxResponseBytes = 8
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: testCredentials()}, WithHTTPClient(transport))
	require.NoError(t, err)
	createContext := &boxCreateConformanceContext{queryContext: queryContext{}}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { createContext.attempt = int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"downloadFile":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.DownloadFile(), testConnection),
		"uploadFile":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UploadFile(), testConnection, scenario.CallID),
		"getFile":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetFile(), testConnection),
		"getFolder":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetFolder(), testConnection),
		"listFolderItems": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListFolderItems(), testConnection),
		"searchFiles":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchFiles(), testConnection),
		"searchFolders":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchFolders(), testConnection),
		"deleteFile":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteFile(), testConnection, scenario.CallID),
		"deleteFolder":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteFolder(), testConnection, scenario.CallID),
		"updateFile":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateFile(), testConnection, scenario.CallID),
		"updateFolder":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateFolder(), testConnection, scenario.CallID),
		"createFolder":    newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateFolder(), testConnection, scenario.CallID),
		"copyFile":        newConformanceMutationInvocation(t, scenario.Input, createContext, client.CopyFile(), testConnection, scenario.CallID),
		"copyFolder":      newConformanceMutationInvocation(t, scenario.Input, createContext, client.CopyFolder(), testConnection, scenario.CallID),
	}}
}

type boxCreateConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *boxCreateConformanceContext) Attempt() int32 { return context.attempt }
func (context *boxCreateConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *boxCreateConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
