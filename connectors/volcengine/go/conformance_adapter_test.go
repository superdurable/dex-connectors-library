// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package volcengine

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
	credentials := Credentials{AccessKeyID: sdkgo.NewSecretString("AKLTtest-access-key"), SecretAccessKey: sdkgo.NewSecretString("test-secret-access-key")}
	if scenario.Name == "describeRegions-defect" {
		credentials.AccessKeyID = sdkgo.NewSecretString("")
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: func() time.Time { return time.Date(2025, 3, 29, 18, 9, 37, 0, time.UTC) }})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"describeRegions": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.DescribeRegions(), testConnection),
	}}
}
