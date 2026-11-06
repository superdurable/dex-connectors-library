// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const conformanceEndpointPath = "/crm/v8"
const conformanceFixturePathPrefix = "/crm/v8"

var conformanceConnection = sdkgo.ConnectionRef{Provider: "zoho", Name: "zoho-crm"}

type conformanceContext struct{ dex.Context }

func (conformanceContext) FlowID() string              { return "zoho-crm-conformance" }
func (conformanceContext) StepExecutionID() string     { return "invoke" }
func (conformanceContext) FlowStartedAt() time.Time    { return time.Unix(1, 0) }
func (conformanceContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (conformanceContext) Done() <-chan struct{}       { return nil }
func (conformanceContext) Err() error                  { return nil }
func (conformanceContext) Value(key any) any           { return context.Background().Value(key) }

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AuthMethodID: USDataCenterAuthMethodID, OAuthClientID: "synthetic-client", OAuthClientSecret: sdkgo.NewSecretString("synthetic-secret"), AccessToken: sdkgo.NewSecretString("synthetic-access"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh"), APIDomain: "https://www.zohoapis.com"}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{conformanceConnection: credentials}, WithAPIBaseURL(endpoint), WithHTTPClient(transport))
	require.NoError(t, err)
	context := conformanceContext{}
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"findRecords":         newConformanceQueryInvocation(t, scenario.Input, context, client.FindRecords(), conformanceConnection),
		"getRecord":           newConformanceQueryInvocation(t, scenario.Input, context, client.GetRecord(), conformanceConnection),
		"listModifiedRecords": newConformanceQueryInvocation(t, scenario.Input, context, client.ListModifiedRecords(), conformanceConnection),
		"listModuleFields":    newConformanceQueryInvocation(t, scenario.Input, context, client.ListModuleFields(), conformanceConnection),
		"upsertRecord":        newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertRecord(), conformanceConnection, scenario.CallID),
		"updateRecord":        newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateRecord(), conformanceConnection, scenario.CallID),
	}}
}
