// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ctyun

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""
const conformanceRequestID = "123e4567-e89b-42d3-a456-426614174000"

type conformanceRequestDependencies struct {
	now       func() time.Time
	requestID func() (string, error)
}

func (dependencies conformanceRequestDependencies) applyClientOption(client *Client) {
	client.now = dependencies.now
	client.requestID = dependencies.requestID
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	config := Config{}
	if scenario.Name == "listKnowledgeBases-oversize" {
		config.MaxResponseBytes = 4
	}
	credentials := Credentials{
		AccessKey:   sdkgo.NewSecretString(testAccessKey),
		SecurityKey: sdkgo.NewSecretString(testSecurityKey),
		TenantID:    sdkgo.NewSecretString(testTenantID),
	}
	if scenario.Name == "listKnowledgeBases-missing-credentials" {
		credentials = Credentials{}
	}
	requestID := func() (string, error) { return conformanceRequestID, nil }
	if scenario.Name == "listKnowledgeBases-id-generation-failure" {
		requestID = func() (string, error) { return "", errors.New("synthetic request ID failure") }
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials},
		WithHTTPClient(transport), conformanceRequestDependencies{conformanceFixedClock(t, scenario), requestID})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listKnowledgeBases": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListKnowledgeBases(), testConnection),
	}}
}
