// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/v2"
const conformanceFixturePathPrefix = "/api/v2"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := clickup.New(clickup.Config{}, sdkgo.StaticCredentialProvider[clickup.Credentials]{
		clickupConnection: {APIToken: sdkgo.NewSecretString(testAPIToken)},
	}, clickup.WithAPIBaseURL(endpoint), clickup.WithHTTPClient(transport), clickup.WithClock(func() time.Time { return time.UnixMilli(1767225600000) }))
	require.NoError(t, err)
	context := newTestDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchTasks":       newConformanceQueryInvocation(t, scenario.Input, context, client.SearchTasks(), clickupConnection),
		"getTask":           newConformanceQueryInvocation(t, scenario.Input, context, client.GetTask(), clickupConnection),
		"createTask":        newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTask(), clickupConnection, scenario.CallID),
		"updateTask":        newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTask(), clickupConnection, scenario.CallID),
		"updateTaskTags":    newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTaskTags(), clickupConnection, scenario.CallID),
		"addComment":        newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), clickupConnection, scenario.CallID),
		"findMemberByEmail": newConformanceQueryInvocation(t, scenario.Input, context, client.FindMemberByEmail(), clickupConnection),
	}}
}
