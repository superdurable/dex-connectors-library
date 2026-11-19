// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package deel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestContractListsExcludePersonalAndCompensationDetails(t *testing.T) {
	contractID := "123e4567-e89b-12d3-a456-426614174000"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "Bearer sample-deel-api-token", request.Header.Get("Authorization"))
		var body string
		switch request.URL.Path {
		case "/api/contracts":
			require.Equal(t, "10", request.URL.Query().Get("limit"))
			body = `{"data":[{"id":"123e4567-e89b-12d3-a456-426614174000","type":"ongoing_time_based","status":"in_progress","worker":{"email":"private@example.com"}}],"page":{"cursor":"after"}}`
		case "/api/contracts/" + contractID + "/tasks":
			body = `{"data":[{"id":"task-123","status":"approved","amount":"private"}]}`
		case "/api/contracts/" + contractID + "/amendments":
			body = `{"data":[{"id":"amendment-123","status":"ACTIVE","sign_status":"APPROVED","rate":5000}],"has_more":true,"cursor":"after"}`
		case "/api/contracts/termination-reasons":
			body = `{"data":[{"id":"reason-123","name":"Project has ended","frequency":"MOST_COMMON"}],"has_more":true,"next_cursor":"after"}`
		default:
			t.Fatalf("unexpected Deel route %s", request.URL.Path)
		}
		_, err := writer.Write([]byte(body))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	contracts, err := sdkgo.RunQuery(queryContext{}, client.ListContracts(), testConnection, ListContractsInput{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, ListContractsBranchListed, contracts.Branch)
	require.Equal(t, ContractPage{Contracts: []ContractStatus{{ID: contractID, Type: "ongoing_time_based", Status: "in_progress"}}, NextCursor: "after"}, contracts.Value)
	tasks, err := sdkgo.RunQuery(queryContext{}, client.ListContractTasks(), testConnection, ListContractTasksInput{ContractID: contractID})
	require.NoError(t, err)
	require.Equal(t, ContractTaskList{Tasks: []ContractTask{{ID: "task-123", Status: "approved"}}}, tasks.Value)
	amendments, err := sdkgo.RunQuery(queryContext{}, client.ListContractAmendments(), testConnection, ListContractAmendmentsInput{ContractID: contractID, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, ContractAmendmentPage{Amendments: []ContractAmendment{{ID: "amendment-123", Status: "ACTIVE", SignStatus: "APPROVED"}}, NextCursor: "after"}, amendments.Value)
	reasons, err := sdkgo.RunQuery(queryContext{}, client.ListTerminationReasons(), testConnection, ListTerminationReasonsInput{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, TerminationReasonPage{Reasons: []TerminationReason{{ID: "reason-123", Name: "Project has ended", Frequency: "MOST_COMMON"}}, NextCursor: "after"}, reasons.Value)
}
