// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	graphTestToken = "graph-token-SENTINEL"
	// The fake's drive IDs have Graph's b! business shape.
	myDriveID   = "b!myDriveKey_1"
	teamDriveID = "b!teamDriveKey-2"
)

var graphConnection = sdkgo.ConnectionRef{Provider: "microsoft", Name: "onedrive-files"}

func newGraph(t *testing.T) *graphfake.Server {
	t.Helper()
	server := graphfake.NewServer(graphfake.Config{AccessToken: graphTestToken, MyDriveID: myDriveID, DriveIDs: []string{myDriveID, teamDriveID}})
	t.Cleanup(server.Close)
	return server
}

func newGraphClient(t *testing.T, endpoint string, config ...onedrive.Config) *onedrive.Client {
	t.Helper()
	return newGraphClientWithMethod(t, endpoint, "", config...)
}

func newGraphClientWithMethod(t *testing.T, endpoint string, authMethodID string, config ...onedrive.Config) *onedrive.Client {
	t.Helper()
	clientConfig := onedrive.Config{}
	if len(config) == 1 {
		clientConfig = config[0]
	}
	clientConfig.Endpoint = endpoint
	client, err := onedrive.New(clientConfig, sdkgo.StaticCredentialProvider[onedrive.Credentials]{
		graphConnection: {AuthMethodID: authMethodID, AccessToken: sdkgo.NewSecretString(graphTestToken)},
	})
	require.NoError(t, err)
	return client
}

// requireSecretFree proves a Result carries neither the access token, a provider message, nor a download URL.
func requireSecretFree(t *testing.T, value any) {
	t.Helper()
	contents, err := json.Marshal(value)
	require.NoError(t, err)
	for _, forbidden := range []string{graphTestToken, "GRAPH-MESSAGE-SENTINEL", "tempauth", "downloadUrl", "/download/"} {
		require.NotContains(t, string(contents), forbidden)
	}
}

func pathHasSuffix(suffix string) func(string) bool {
	return func(path string) bool { return strings.HasSuffix(path, suffix) }
}

func anyPath(string) bool { return true }

type dexContext struct {
	context.Context
	step string
}

func newDexContext(step string) *dexContext {
	return &dexContext{Context: context.Background(), step: step}
}
func (*dexContext) FlowID() string                                  { return "onedrive-flow" }
func (*dexContext) RunID() string                                   { return "run" }
func (*dexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *dexContext) StepExecutionID() string                 { return context.step }
func (*dexContext) FromStepExecutionID() string                     { return "" }
func (*dexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*dexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*dexContext) Attempt() int32                                  { return 1 }
func (*dexContext) HasTimerFired() bool                             { return false }
func (*dexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*dexContext) WaitForMethodFailed() bool                       { return false }
func (*dexContext) RecordHeartbeat(any) error                       { return nil }
func (*dexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*dexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*dexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*dexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*dexContext)(nil)
