// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lever

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateOpportunityStageClassifiesWriteResponses(t *testing.T) {
	const opportunityID = "250d8f03-738a-4bba-a671-8a3d73477145"
	const stageID = "00922a60-7c15-422b-b086-f62000824fd7"
	for _, testCase := range []struct {
		name    string
		status  int
		branch  sdkgo.BranchID
		retries bool
	}{
		{name: "updated", status: 204, branch: UpdateOpportunityStageBranchUpdated},
		{name: "rejected", status: 403, branch: UpdateOpportunityStageBranchProviderRejected},
		{name: "missing", status: 404, branch: UpdateOpportunityStageBranchNotFound},
		{name: "rate limit", status: 429, retries: true},
		{name: "unavailable", status: 503, retries: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			writeCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.Method {
				case http.MethodGet:
					stage := "fff60592-31dd-4ebe-ba8e-e7a397c30f8e"
					if writeCount > 0 && testCase.status == 204 {
						stage = stageID
					}
					_, err := writer.Write([]byte(`{"data":{"id":"` + opportunityID + `","name":"Sample Candidate","stage":"` + stage + `"}}`))
					require.NoError(t, err)
				case http.MethodPut:
					writeCount++
					require.Equal(t, "/api/opportunities/"+opportunityID+"/stage", request.URL.Path)
					writer.WriteHeader(testCase.status)
				default:
					t.Errorf("unexpected method %s", request.Method)
				}
			}))
			defer server.Close()
			result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL+"/api", 0).UpdateOpportunityStage(), testConnection, UpdateOpportunityStageInput{OpportunityID: opportunityID, StageID: stageID})
			require.Equal(t, 1, writeCount)
			if testCase.retries {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, err.Error(), "sample-lever-api-key")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if result.Failure != nil {
				require.False(t, strings.Contains(result.Failure.Message, "sample-lever-api-key"))
			}
		})
	}
}

func TestSetOpportunityArchiveStateClassifiesWriteResponses(t *testing.T) {
	const opportunityID = "250d8f03-738a-4bba-a671-8a3d73477145"
	const reasonID = "63dd55b2-a99f-4e7b-985f-22c7bf80ab42"
	for _, testCase := range []struct {
		name    string
		status  int
		branch  sdkgo.BranchID
		retries bool
	}{
		{name: "updated", status: 204, branch: SetOpportunityArchiveStateBranchUpdated},
		{name: "rejected", status: 403, branch: SetOpportunityArchiveStateBranchProviderRejected},
		{name: "missing", status: 404, branch: SetOpportunityArchiveStateBranchNotFound},
		{name: "rate limit", status: 429, retries: true},
		{name: "unavailable", status: 503, retries: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			writeCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.Method {
				case http.MethodGet:
					body := `{"data":{"id":"` + opportunityID + `","archiveReason":null,"archivedAt":null}}`
					if writeCount > 0 && testCase.status == 204 {
						body = `{"data":{"id":"` + opportunityID + `","archiveReason":"` + reasonID + `","archivedAt":1793100000000}}`
					}
					_, err := writer.Write([]byte(body))
					require.NoError(t, err)
				case http.MethodPut:
					writeCount++
					require.Equal(t, "/api/opportunities/"+opportunityID+"/archived", request.URL.Path)
					writer.WriteHeader(testCase.status)
				default:
					t.Errorf("unexpected method %s", request.Method)
				}
			}))
			defer server.Close()
			result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL+"/api", 0).SetOpportunityArchiveState(), testConnection, SetOpportunityArchiveStateInput{OpportunityID: opportunityID, ReasonID: reasonID})
			require.Equal(t, 1, writeCount)
			if testCase.retries {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, err.Error(), "sample-lever-api-key")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if result.Failure != nil {
				require.False(t, strings.Contains(result.Failure.Message, "sample-lever-api-key"))
			}
		})
	}
}
