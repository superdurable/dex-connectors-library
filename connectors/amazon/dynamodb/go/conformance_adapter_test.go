// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package dynamodb

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
	"time"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "amazon-dynamodb", Name: "amazon-dynamodb"}
	credentials := Credentials{AccessKeyID: sdkgo.NewSecretString("AKIADYNAMOCONNECTOR"), SecretAccessKey: sdkgo.NewSecretString("dynamodb-test-secret"), SessionToken: sdkgo.NewSecretString("")}
	client, err := New(Config{Endpoint: endpoint, Region: "eu-west-1"}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport), signingTimeOption{now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"getItem": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetItem(), reference)}}
}
