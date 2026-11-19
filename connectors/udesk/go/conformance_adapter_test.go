// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package udesk

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type conformanceRequestDependencies struct {
	clock  func() time.Time
	random io.Reader
}

func (dependencies conformanceRequestDependencies) applyClientOption(client *Client) {
	client.requestClock = dependencies.clock
	client.requestRandom = dependencies.random
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AdminEmail: sdkgo.NewSecretString("REDACTED_ADMIN@example.invalid"), APIToken: sdkgo.NewSecretString("REDACTED_API_TOKEN")}
	if scenario.Name == "listTicketStatuses-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials},
		WithHTTPClient(transport), conformanceRequestDependencies{conformanceFixedClock(t, scenario), newConformanceSyntheticRandomReader(scenario.RandomSeed)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listTicketStatuses": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTicketStatuses(), testConnection),
	}}
}
