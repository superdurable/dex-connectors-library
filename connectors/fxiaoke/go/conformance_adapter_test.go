// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fxiaoke

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AppID: "FSAID_123", AppSecret: sdkgo.NewSecretString("app-secret"), PermanentCode: sdkgo.NewSecretString("permanent-code")}
	if scenario.Name == "listObjectMetadata-defect" {
		credentials = Credentials{}
	}
	config := Config{EmployeeID: "employee-123"}
	if scenario.Name == "listObjectMetadata-oversized" {
		config.MaxResponseBytes = 16
	}
	options := []Option{WithHTTPClient(transport)}
	if scenario.RandomSeed != "" {
		options = append(options, nonceReaderOption{reader: newConformanceSyntheticRandomReader(scenario.RandomSeed)})
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, options...)
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"listObjectMetadata": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListObjectMetadata(), testConnection)}}
}
