// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sfexpress

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{PartnerID: "customer-1", Checkword: sdkgo.NewSecretString("private checkword")}
	if scenario.Name == "queryRoutes-defect" {
		credentials = Credentials{}
	}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "queryRoutes-oversized" {
		config.MaxResponseBytes = 16
	}
	options := []Option{WithHTTPClient(transport), clockOption{now: func() time.Time { return time.Unix(1731510000, 0) }}}
	if scenario.RandomSeed != "" {
		options = append(options, requestIDReaderOption{reader: newConformanceSyntheticRandomReader(scenario.RandomSeed)})
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, options...)
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"queryRoutes": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.QueryRoutes(), testConnection),
	}}
}
