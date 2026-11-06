// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const conformanceEndpointPath = "/v1.0"
const conformanceFixturePathPrefix = "/v1.0"

var conformanceConnection = sdkgo.ConnectionRef{Provider: "microsoft", Name: "entra-conformance"}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	options := []Option{WithHTTPClient(transport), WithLocalProviderURL(strings.TrimSuffix(endpoint, conformanceEndpointPath))}
	if scenario.RandomSeed != "" {
		options = append(options, withInitialPasswordRandomSource(newConformanceSyntheticRandomReader(scenario.RandomSeed)))
	}
	config := Config{}
	if scenario.Name == "removeUserFromGroup-invalid-response" {
		config.MaxResponseBytes = 2
	}
	var credentials CredentialSource = sdkgo.StaticCredentialProvider[Credentials]{
		conformanceConnection: {AuthMethodID: EntraAppOnlyAuthMethodID, AccessToken: sdkgo.NewSecretString("entra-token")},
	}
	if scenario.Name == "getUser-401-renewed" {
		credentials = conformanceRenewingCredentials{}
	}
	client, err := New(config, credentials, options...)
	require.NoError(t, err)
	firstAttemptAt := time.Now().Add(-2 * time.Minute)
	if strings.Contains(scenario.Name, "propagation") {
		firstAttemptAt = time.Now()
	}
	context := &conformanceDexContext{Context: context.Background(), firstAttemptAt: firstAttemptAt}
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getUser":              newConformanceQueryInvocation(t, scenario.Input, context, client.GetUser(), conformanceConnection),
		"listUsers":            newConformanceQueryInvocation(t, scenario.Input, context, client.ListUsers(), conformanceConnection),
		"createUser":           newConformanceMutationInvocation(t, scenario.Input, context, client.CreateUser(), conformanceConnection, scenario.CallID),
		"disableUser":          newConformanceMutationInvocation(t, scenario.Input, context, client.DisableUser(), conformanceConnection, scenario.CallID),
		"enableUser":           newConformanceMutationInvocation(t, scenario.Input, context, client.EnableUser(), conformanceConnection, scenario.CallID),
		"revokeSignInSessions": newConformanceMutationInvocation(t, scenario.Input, context, client.RevokeSignInSessions(), conformanceConnection, scenario.CallID),
		"addUserToGroup":       newConformanceMutationInvocation(t, scenario.Input, context, client.AddUserToGroup(), conformanceConnection, scenario.CallID),
		"removeUserFromGroup":  newConformanceMutationInvocation(t, scenario.Input, context, client.RemoveUserFromGroup(), conformanceConnection, scenario.CallID),
	}}
}

type conformanceDexContext struct {
	context.Context
	firstAttemptAt time.Time
}

func (*conformanceDexContext) FlowID() string                                  { return "entra-conformance-flow" }
func (*conformanceDexContext) RunID() string                                   { return "run" }
func (*conformanceDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (*conformanceDexContext) StepExecutionID() string                         { return "entra-conformance" }
func (*conformanceDexContext) FromStepExecutionID() string                     { return "" }
func (*conformanceDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (c *conformanceDexContext) FirstAttemptAt() time.Time                     { return c.firstAttemptAt }
func (*conformanceDexContext) Attempt() int32                                  { return 1 }
func (*conformanceDexContext) HasTimerFired() bool                             { return false }
func (*conformanceDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*conformanceDexContext) WaitForMethodFailed() bool                       { return false }
func (*conformanceDexContext) RecordHeartbeat(any) error                       { return nil }
func (*conformanceDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*conformanceDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*conformanceDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*conformanceDexContext) RecordEvent(string, any) error                   { return nil }

// conformanceRenewingCredentials proves one provider 401 causes a single token refresh.
type conformanceRenewingCredentials struct{}

func (conformanceRenewingCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	return Credentials{AuthMethodID: MicrosoftOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("stale-token")}, nil
}

func (conformanceRenewingCredentials) ResolveAfterRejection(context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return Credentials{AuthMethodID: MicrosoftOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("fresh-token")}, nil
}

func (provider conformanceRenewingCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return provider.Resolve(call)
}
