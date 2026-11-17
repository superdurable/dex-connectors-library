// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	connection := sdkgo.ConnectionRef{Provider: "resend", Name: "resend"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{connection: {APIKey: sdkgo.NewSecretString("test-secret-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	createContext := &resendCreateConformanceContext{attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { createContext.heartbeat = value })
	createContext.attempt = priorAttempt + 1
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			createContext.attempt = priorAttempt + int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"sendEmail":            newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.SendEmail(), connection, scenario.CallID),
		"getEmail":             newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetEmail(), connection),
		"listEmails":           newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListEmails(), connection),
		"getContact":           newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetContact(), connection),
		"listContacts":         newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListContacts(), connection),
		"getDomain":            newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetDomain(), connection),
		"listDomains":          newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListDomains(), connection),
		"updateContact":        newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateContact(), connection, scenario.CallID),
		"deleteContact":        newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteContact(), connection, scenario.CallID),
		"deleteDomain":         newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteDomain(), connection, scenario.CallID),
		"updateScheduledEmail": newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateScheduledEmail(), connection, scenario.CallID),
		"cancelScheduledEmail": newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.CancelScheduledEmail(), connection, scenario.CallID),
		"createContact":        newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateContact(), connection, scenario.CallID),
		"createDomain":         newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateDomain(), connection, scenario.CallID),
		"updateDomain":         newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateDomain(), connection, scenario.CallID),
		"verifyDomain":         newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.VerifyDomain(), connection, scenario.CallID),
	}}
}

type resendCreateConformanceContext struct {
	providerTestContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *resendCreateConformanceContext) Attempt() int32 { return context.attempt }
func (context *resendCreateConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *resendCreateConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
