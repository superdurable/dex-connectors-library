// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package brevo

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v3"
const conformanceFixturePathPrefix = ""

type conformanceHTTPClientOption struct{ client *http.Client }

type brevoConformanceContext struct {
	providerContext
	attempt  int32
	prior    json.RawMessage
	recorded json.RawMessage
}

func (context *brevoConformanceContext) Attempt() int32 { return context.attempt }
func (context *brevoConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.prior) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.prior, value)
}
func (context *brevoConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recorded = encoded
	return nil
}

func (option conformanceHTTPClientOption) applyClientOption(client *Client) {
	client.httpClient = option.client
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString("brevo-test-key")},
	}, conformanceHTTPClientOption{client: transport})
	require.NoError(t, err)
	context := &brevoConformanceContext{attempt: 1}
	if len(scenario.PriorHeartbeat) > 0 {
		context.attempt = conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.prior = value }) + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.prior = append(json.RawMessage(nil), context.recorded...)
			context.recorded = nil
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"createOrUpdateContact": newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.CreateOrUpdateContact(), testConnection, scenario.CallID),
		"getContact":            newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.GetContact(), testConnection),
		"listContacts":          newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListContacts(), testConnection),
		"updateContact":         newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.UpdateContact(), testConnection, scenario.CallID),
		"deleteContact":         newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.DeleteContact(), testConnection, scenario.CallID),
		"listAttributes":        newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListAttributes(), testConnection),
		"listSenders":           newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListSenders(), testConnection),
		"createSender":          newConformanceMutationInvocation(t, scenario.Input, context, client.CreateSender(), testConnection, scenario.CallID),
		"updateSender":          newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.UpdateSender(), testConnection, scenario.CallID),
		"deleteSender":          newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.DeleteSender(), testConnection, scenario.CallID),
	}}
}
