// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package easyapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	config := Config{BaseURL: endpoint}
	if scenario.Name == "queryShop-oversize" {
		config.MaxResponseBytes = 5
	}
	credentials := Credentials{AppKey: sdkgo.NewSecretString("test-key"), AppSecret: sdkgo.NewSecretString("private-secret")}
	if scenario.Name == "queryShop-missing-credentials" {
		credentials = Credentials{}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"queryShop": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.QueryShop(), testConnection),
	}}
}

func TestConformanceOutputsExcludePrivateCompanyFields(t *testing.T) {
	data, err := os.ReadFile("../../../conformance/connectors/easyapi/scenarios.json")
	require.NoError(t, err)
	var fixture struct {
		Scenarios []struct {
			Expected json.RawMessage `json:"expected"`
		} `json:"scenarios"`
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	for _, scenario := range fixture.Scenarios {
		recorded := string(scenario.Expected)
		for _, privateValue := range []string{"test-key", "private-secret", "91320211MA1WML8X6T", "private tax", "private address", "private phone", "private bank", "private staff", "private certificate"} {
			require.False(t, strings.Contains(recorded, privateValue), "recorded result contains private value")
		}
	}
}
