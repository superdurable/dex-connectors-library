// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendgrid

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
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	mutationContext := &sendgridConformanceContext{attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { mutationContext.heartbeat = value })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { mutationContext.attempt = priorAttempt + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"getStats":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetStats(), testConnection),
		"listTemplates":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTemplates(), testConnection),
		"getContact":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), testConnection),
		"searchContacts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchContacts(), testConnection),
		"listContacts":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), testConnection),
		"upsertContact":  newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.UpsertContact(), testConnection, scenario.CallID),
		"deleteContact":  newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.DeleteContact(), testConnection, scenario.CallID),
		"getList":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetList(), testConnection),
		"listLists":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListLists(), testConnection),
		"updateList":     newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.UpdateList(), testConnection, scenario.CallID),
		"deleteList":     newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.DeleteList(), testConnection, scenario.CallID),
		"createList":     newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateList(), testConnection, scenario.CallID),
		"sendMail":       newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.SendMail(), testConnection, scenario.CallID),
	}}
}

type sendgridConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *sendgridConformanceContext) Attempt() int32 { return context.attempt }

func (context *sendgridConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}

func (context *sendgridConformanceContext) GetLastHeartbeatValue(destination any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, destination)
}
