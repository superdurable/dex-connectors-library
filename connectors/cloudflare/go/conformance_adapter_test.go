// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudflare

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/client/v4"
const conformanceFixturePathPrefix = "/client/v4"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	token := "test-apitoken"
	if scenario.Name == "listZones-defect" {
		token = ""
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIToken: sdkgo.NewSecretString(token)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := testContext{}
	createContext := &cloudflareCreateConformanceContext{testContext: context, attempt: 1}
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { createContext.heartbeat = value })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { createContext.attempt = prior + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"createDNSRecord":  newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateDNSRecord(), testConnection, scenario.CallID),
		"listZones":        newConformanceQueryInvocation(t, scenario.Input, context, client.ListZones(), testConnection),
		"purgeCacheByURLs": newConformanceMutationInvocation(t, scenario.Input, context, client.PurgeCacheByURLs(), testConnection, scenario.CallID),
		"purgeEntireCache": newConformanceMutationInvocation(t, scenario.Input, context, client.PurgeEntireCache(), testConnection, scenario.CallID),
		"listDNSRecords":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListDNSRecords(), testConnection),
		"getDNSRecord":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetDNSRecord(), testConnection),
		"updateDNSRecord":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateDNSRecord(), testConnection, scenario.CallID),
		"deleteDNSRecord":  newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteDNSRecord(), testConnection, scenario.CallID),
	}}
}

type cloudflareCreateConformanceContext struct {
	testContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *cloudflareCreateConformanceContext) Attempt() int32 { return context.attempt }

func (context *cloudflareCreateConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}

func (context *cloudflareCreateConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
