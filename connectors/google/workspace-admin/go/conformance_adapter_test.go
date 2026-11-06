// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin_test

import (
	"bytes"
	"crypto/rand"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/admin/directory/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	if scenario.Operation == "createUser" {
		// Fix generated passwords so exact request bodies replay across languages.
		originalReader := rand.Reader
		rand.Reader = bytes.NewReader(bytes.Repeat([]byte{0x42}, 32*100))
		t.Cleanup(func() { rand.Reader = originalReader })
	}
	client, err := workspaceadmin.New(workspaceadmin.Config{Endpoint: endpoint, MaxResponseBytes: 512},
		sdkgo.StaticCredentialProvider[workspaceadmin.Credentials]{
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
