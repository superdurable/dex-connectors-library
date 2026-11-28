// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fanyigou

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AppID: sdkgo.NewSecretString("test-app"), PrivateKey: sdkgo.NewSecretString("test-private-key")}
	if scenario.Name == "getAccountBalance-defect" {
		credentials = Credentials{}
	}
	config := Config{}
	if scenario.Name == "getAccountBalance-oversized" {
		config.MaxResponseBytes = 16
	}
	options := []Option{WithHTTPClient(transport)}
	if scenario.RandomSeed != "" {
		options = append(options, nonceReaderOption{reader: newConformanceSyntheticRandomReader(scenario.RandomSeed)})
	}
	if scenario.Name == "getAccountBalance-nonceUnavailable" {
		options = append(options, nonceReaderOption{reader: strings.NewReader("")})
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, options...)
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"getAccountBalance": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccountBalance(), testConnection)}}
}
