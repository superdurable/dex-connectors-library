// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qiniu

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
	credentials := Credentials{AccessKey: sdkgo.NewSecretString(testAccessKey), SecretKey: sdkgo.NewSecretString(testSecretKey)}
	if scenario.Name == "listBuckets-defect" {
		credentials = Credentials{}
	}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "listBuckets-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), WithClock(func() time.Time { return testNow }))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listBuckets": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListBuckets(), testConnection),
	}}
}
