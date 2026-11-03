// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/go"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := snowflake.New(
		snowflake.Config{AccountIdentifier: "myorg-analytics", Warehouse: "REPORTING_WH", Role: "DEX_REPORTING", Database: "ANALYTICS", Schema: "BILLING", MaxRows: 1},
		sdkgo.StaticCredentialProvider[snowflake.Credentials]{testConnection: tokenCredentials()},
		snowflake.WithLocalProviderURL(endpoint), snowflake.WithHTTPClient(transport),
		snowflake.WithClock(func() time.Time { return fixedNow }),
	)
	require.NoError(t, err)
	context := testsupport.NewDexContext("snowflake-conformance", "statement-step")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"submitStatement":    newConformanceMutationInvocation(t, scenario.Input, context, client.SubmitStatement(), testConnection, scenario.CallID),
		"getStatementResult": newConformanceQueryInvocation(t, scenario.Input, context, client.GetStatementResult(), testConnection),
		"cancelStatement":    newConformanceMutationInvocation(t, scenario.Input, context, client.CancelStatement(), testConnection, scenario.CallID),
	}}
}
