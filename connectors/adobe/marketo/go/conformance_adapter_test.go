// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/rest"
const conformanceFixturePathPrefix = "/rest"

type reauthorizationCredentialProvider struct{}

func (reauthorizationCredentialProvider) Resolve(sdkgo.Call) (Credentials, error) {
	return Credentials{}, sdkgo.NewReauthorizationRequiredError(errors.New("connection revoked"))
}

func (provider reauthorizationCredentialProvider) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return provider.Resolve(call)
}

type conformanceMarketoRetryContext struct {
	queryContext
	attempt   int32
	heartbeat []byte
}

func (context *conformanceMarketoRetryContext) Attempt() int32 { return context.attempt }
func (context *conformanceMarketoRetryContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *conformanceMarketoRetryContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	_ = endpoint // The injected transport intercepts the fixed HTTPS subscription URL.
	reference := sdkgo.ConnectionRef{Provider: "adobe", Name: "adobe-marketo"}
	client, err := New(Config{
		RESTEndpoint:  "https://123-ABC-456.mktorest.com/rest",
		TokenEndpoint: "https://123-ABC-456.mktorest.com/identity/oauth/token",
	}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {
			ClientID: "sample-client-id", ClientSecret: sdkgo.NewSecretString("sample-client-secret"),
			AccessToken: sdkgo.NewSecretString("sample-access-token"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	if strings.HasSuffix(scenario.Name, "-credentialLostAfterUnknown") {
		client.credentials = reauthorizationCredentialProvider{}
	}
	retryContext := &conformanceMarketoRetryContext{attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(checkpoint []byte) { retryContext.heartbeat = checkpoint })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { retryContext.attempt = priorAttempt + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"getLead":                    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetLead(), reference),
		"findLeadsByEmail":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.FindLeadsByEmail(), reference),
		"updateLead":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateLead(), reference, scenario.CallID),
		"createLead":                 newConformanceMutationInvocation(t, scenario.Input, retryContext, client.CreateLead(), reference, scenario.CallID),
		"upsertLeadByEmail":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertLeadByEmail(), reference, scenario.CallID),
		"getActivityPagingToken":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetActivityPagingToken(), reference),
		"listActivityTypes":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListActivityTypes(), reference),
		"listActivities":             newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListActivities(), reference),
		"getCompanyByExternalID":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCompanyByExternalID(), reference),
		"createCompany":              newConformanceMutationInvocation(t, scenario.Input, retryContext, client.CreateCompany(), reference, scenario.CallID),
		"upsertCompany":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertCompany(), reference, scenario.CallID),
		"deleteCompany":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteCompany(), reference, scenario.CallID),
		"getOpportunityByExternalID": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOpportunityByExternalID(), reference),
		"createOpportunity":          newConformanceMutationInvocation(t, scenario.Input, retryContext, client.CreateOpportunity(), reference, scenario.CallID),
		"upsertOpportunity":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertOpportunity(), reference, scenario.CallID),
		"deleteOpportunity":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteOpportunity(), reference, scenario.CallID),
	}}
}
