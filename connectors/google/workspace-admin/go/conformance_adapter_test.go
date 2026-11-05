// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin_test

import (
	"crypto/rand"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/admin/directory/v1"

type fixedConformancePasswordReader struct{}

func (fixedConformancePasswordReader) Read(value []byte) (int, error) {
	for index := range value {
		value[index] = 0x42
	}
	return len(value), nil
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	// A fixed fake password makes generated request bodies reproducible across Go and TypeScript replay.
	previousReader := rand.Reader
	rand.Reader = fixedConformancePasswordReader{}
	t.Cleanup(func() { rand.Reader = previousReader })
	config := workspaceadmin.Config{Endpoint: endpoint}
	if strings.HasSuffix(scenario.Name, "oversized") {
		config.MaxResponseBytes = 1
	}
	client, err := workspaceadmin.New(config, sdkgo.StaticCredentialProvider[workspaceadmin.Credentials]{
		testConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)},
	}, workspaceadmin.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newTestDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getUser":             newConformanceQueryInvocation(t, scenario.Input, context, client.GetUser(), testConnection),
		"listUsers":           newConformanceQueryInvocation(t, scenario.Input, context, client.ListUsers(), testConnection),
		"createUser":          newConformanceMutationInvocation(t, scenario.Input, context, client.CreateUser(), testConnection, scenario.CallID),
		"suspendUser":         newConformanceMutationInvocation(t, scenario.Input, context, client.SuspendUser(), testConnection, scenario.CallID),
		"unsuspendUser":       newConformanceMutationInvocation(t, scenario.Input, context, client.UnsuspendUser(), testConnection, scenario.CallID),
		"addUserToGroup":      newConformanceMutationInvocation(t, scenario.Input, context, client.AddUserToGroup(), testConnection, scenario.CallID),
		"removeUserFromGroup": newConformanceMutationInvocation(t, scenario.Input, context, client.RemoveUserFromGroup(), testConnection, scenario.CallID),
	}}
}
