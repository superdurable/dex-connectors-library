// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package siliconflow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"strings"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	key := "test-siliconflow-key-123"
	if scenario.Name == "listModels-defect" {
		key = strings.Repeat("x", 513)
	}
	credentials := Credentials{APIKey: sdkgo.NewSecretString(key)}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "listModels-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listModels": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListModels(), testConnection),
	}}
}
