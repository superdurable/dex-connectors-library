// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kingsoftcloud

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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {
		AccessKeyID: sdkgo.NewSecretString(testAccessKeyID),
		SecretAccessKey: sdkgo.NewSecretString(testSecretAccessKey),
	}}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "listUsers-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, credentials, WithHTTPClient(transport), clockOption{now: func() time.Time {
		return time.Date(2021, 8, 12, 2, 47, 36, 0, time.UTC)
	}})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listUsers": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListUsers(), testConnection),
	}}
}
