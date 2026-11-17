// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/developer_api/v1"
const conformanceFixturePathPrefix = "/developer_api/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "copper", Name: "copper"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token"), UserEmail: sdkgo.NewSecretString("user@example.com")}}
	if scenario.Name == "listLeadStatuses-defect" || scenario.Name == "listCustomerSources-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &personConformanceContext{attempt: 1}
	if len(scenario.PriorHeartbeat) > 0 {
		context.heartbeat = append([]byte(nil), scenario.PriorHeartbeat...)
		var prior struct {
			Attempt int `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(context.heartbeat, &prior))
		context.attempt = int32(prior.Attempt + 1)
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"createLead":        newConformanceMutationInvocation(t, scenario.Input, context, client.CreateLead(), reference, scenario.CallID),
		"listLeads":         newConformanceQueryInvocation(t, scenario.Input, context, client.ListLeads(), reference),
		"updateLead":        newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateLead(), reference, scenario.CallID),
		"deleteLead":        newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteLead(), reference, scenario.CallID),
		"createOpportunity": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateOpportunity(), reference, scenario.CallID),
		"getOpportunity": newConformanceQueryInvocation(t, scenario.Input, context, client.GetOpportunity(), reference),
		"listOpportunities": newConformanceQueryInvocation(t, scenario.Input, context, client.ListOpportunities(), reference),
		"updateOpportunity": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateOpportunity(), reference, scenario.CallID),
		"deleteOpportunity": newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteOpportunity(), reference, scenario.CallID),
		"createProject":     newConformanceMutationInvocation(t, scenario.Input, context, client.CreateProject(), reference, scenario.CallID),
		"getProject":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetProject(), reference),
		"listProjects":      newConformanceQueryInvocation(t, scenario.Input, context, client.ListProjects(), reference),
		"updateProject":     newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateProject(), reference, scenario.CallID),
		"deleteProject":     newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteProject(), reference, scenario.CallID),
		"createTask":        newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTask(), reference, scenario.CallID),
		"getTask":           newConformanceQueryInvocation(t, scenario.Input, context, client.GetTask(), reference),
		"listTasks":         newConformanceQueryInvocation(t, scenario.Input, context, client.ListTasks(), reference),
		"updateTask":        newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTask(), reference, scenario.CallID),
		"deleteTask":        newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteTask(), reference, scenario.CallID),
		"listUsers":         newConformanceQueryInvocation(t, scenario.Input, context, client.ListUsers(), reference),
		"listCustomerSources": newConformanceQueryInvocation(t, scenario.Input, context, client.ListCustomerSources(), reference),
		"createCompany":       newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCompany(), reference, scenario.CallID),
		"getCompany":          newConformanceQueryInvocation(t, scenario.Input, context, client.GetCompany(), reference),
		"listCompanies":       newConformanceQueryInvocation(t, scenario.Input, context, client.ListCompanies(), reference),
		"updateCompany":       newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateCompany(), reference, scenario.CallID),
		"deleteCompany":       newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteCompany(), reference, scenario.CallID),
		"listLeadStatuses":    newConformanceQueryInvocation(t, scenario.Input, context, client.ListLeadStatuses(), reference),
		"getLead":             newConformanceQueryInvocation(t, scenario.Input, context, client.GetLead(), reference),
		"createPerson":        newConformanceMutationInvocation(t, scenario.Input, context, client.CreatePerson(), reference, scenario.CallID),
		"getPerson":           newConformanceQueryInvocation(t, scenario.Input, context, client.GetPerson(), reference),
		"listPeople":          newConformanceQueryInvocation(t, scenario.Input, context, client.ListPeople(), reference),
		"updatePerson":        newConformanceMutationInvocation(t, scenario.Input, context, client.UpdatePerson(), reference, scenario.CallID),
		"deletePerson":        newConformanceMutationInvocation(t, scenario.Input, context, client.DeletePerson(), reference, scenario.CallID),
	}}
}

type personConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat []byte
}

func (context *personConformanceContext) Attempt() int32 { return context.attempt }
func (context *personConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.heartbeat = encoded
	}
	return err
}
func (context *personConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
