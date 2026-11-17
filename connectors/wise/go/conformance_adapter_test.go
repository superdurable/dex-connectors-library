// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wise

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

type wiseQuoteContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (ctx *wiseQuoteContext) Attempt() int32 {
	if ctx.attempt == 0 {
		return 1
	}
	return ctx.attempt
}

func (ctx *wiseQuoteContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx.heartbeat = encoded
	return nil
}

func (ctx *wiseQuoteContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(ctx.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(ctx.heartbeat, target)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "wise", Name: "wise"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIToken: sdkgo.NewSecretString("sample-wise-api-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	quoteContext := &wiseQuoteContext{heartbeat: append(json.RawMessage(nil), scenario.PriorHeartbeat...)}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { quoteContext.attempt = int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"listProfiles":                   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProfiles(), reference),
		"getProfile":                     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetProfile(), reference),
		"listRecipients":                 newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListRecipients(), reference),
		"listBalances":                   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListBalances(), reference),
		"listPayinCurrencies":            newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPayinCurrencies(), reference),
		"listAvailableBalanceCurrencies": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAvailableBalanceCurrencies(), reference),
		"getExchangeRates":               newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetExchangeRates(), reference),
		"getQuote":                       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetQuote(), reference),
		"createQuote":                    newConformanceMutationInvocation(t, scenario.Input, quoteContext, client.CreateQuote(), reference, scenario.CallID),
		"createTransfer":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateTransfer(), reference, scenario.CallID),
		"cancelTransfer":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CancelTransfer(), reference, scenario.CallID),
		"getTransfer":                    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTransfer(), reference),
	}}
}
