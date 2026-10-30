// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	commentPageJSON = `{"start":0,"limit":2,"size":2,"isLastPage":false,"values":[` +
		`{"id":"1000","body":"Can you try safe mode?","public":true,"author":{"accountId":"5b10ac8d82e05b22cc7d4ef5","displayName":"Ada","emailAddress":"SENTINEL-ada@example.com"},"created":{"epochMillis":1790000000000}},` +
		`{"id":"1001","body":"Disk is failing; order a replacement.","public":false,"author":{"accountId":"5b10ac8d82e05b22cc7d4ef5"},"created":{"iso8601":"2026-09-21T15:00:00+0000"}}]}`
	slaPageJSON = `{"start":0,"limit":50,"size":2,"isLastPage":true,"values":[` +
		`{"id":"1","name":"Time to first response","completedCycles":[{"startTime":{"epochMillis":1790000000000},"stopTime":{"epochMillis":1790000600000},` +
		`"breachTime":{"epochMillis":1790003600000},"breached":false,"goalDuration":{"millis":3600000},"elapsedTime":{"millis":600000},"remainingTime":{"millis":3000000}}]},` +
		`{"id":"2","name":"Time to resolution","ongoingCycle":{"startTime":{"epochMillis":1790000000000},"breachTime":{"epochMillis":1790028800000},` +
		`"breached":true,"paused":true,"withinCalendarHours":true,"goalDuration":{"millis":28800000},"elapsedTime":{"millis":30000000},"remainingTime":{"millis":-1200000}}}]}`
)

func TestGetTicketReadsTheRequestCommentsAndSLAs(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/comment"):
			writeJSON(t, response, http.StatusOK, commentPageJSON)
		case strings.HasSuffix(request.URL.Path, "/sla"):
			writeJSON(t, response, http.StatusOK, slaPageJSON)
		default:
			writeJSON(t, response, http.StatusOK, issueJSON("10042", "ITH-42", "Laptop will not boot", `["hardware"]`))
		}
	})
	result, err := sdkgo.RunQuery(newTestDexContext("get"), newTestClient(t, provider.URL).GetTicket(), jsmConnection,
		jiraservicemanagement.GetTicketInput{IssueIDOrKey: "ith-42", CommentLimit: 2})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.GetTicketBranchFound, result.Branch)
	ticket := result.Value.Ticket
	require.Equal(t, "ITH-42", ticket.Key)
	require.Equal(t, "ITH", ticket.ProjectKey)
	require.Equal(t, "My laptop will not boot.\nError 0x7B.", ticket.Description)
	require.Equal(t, jiraservicemanagement.StatusCategoryToDo, ticket.Status.CategoryKey)
	require.Equal(t, testCustomerAccount, ticket.Reporter.AccountID)
	require.Equal(t, []string{"hardware"}, ticket.Labels)
	require.Equal(t, "Medium", ticket.Priority.Name)

	require.Len(t, result.Value.Comments, 2)
	require.True(t, result.Value.Comments[0].IsPublic)
	require.False(t, result.Value.Comments[1].IsPublic, "an internal note is marked as such")
	require.Equal(t, time.Date(2026, time.September, 21, 15, 0, 0, 0, time.UTC), result.Value.Comments[1].CreatedAt)
	require.True(t, result.Value.HasMoreComments)

	require.Len(t, result.Value.SLAs, 2)
	firstResponse := result.Value.SLAs[0]
	require.Equal(t, "Time to first response", firstResponse.Name)
	require.Nil(t, firstResponse.OngoingCycle)
	require.Len(t, firstResponse.CompletedCycles, 1)
	require.Equal(t, int64(600000), firstResponse.CompletedCycles[0].ElapsedMillis)
	require.Equal(t, time.UnixMilli(1790000600000).UTC(), *firstResponse.CompletedCycles[0].StoppedAt)
	resolution := result.Value.SLAs[1].OngoingCycle
	require.True(t, resolution.IsBreached)
	require.True(t, resolution.IsPaused)
	require.Equal(t, int64(-1200000), resolution.RemainingMillis)
	require.Equal(t, time.UnixMilli(1790028800000).UTC(), *resolution.BreachAt)
	requireNoSentinel(t, result)

	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, testPlatformPrefix+"/issue/ith-42", provider.request(0).path)
	require.Contains(t, provider.request(0).rawQuery, "description")
	require.Equal(t, testServiceDeskPath+"/request/ITH-42/comment", provider.request(1).path, "later reads use the current key")
	require.Equal(t, "internal=true&limit=2&public=true&start=0", provider.request(1).rawQuery)
	require.Equal(t, testServiceDeskPath+"/request/ITH-42/sla", provider.request(2).path)
}

func TestGetTicketMapsEachReadFailure(t *testing.T) {
	for _, test := range []struct {
		name    string
		failing string
		status  int
		branch  sdkgo.BranchID
	}{
		{"missing issue", "issue", http.StatusNotFound, jiraservicemanagement.GetTicketBranchNotFound},
		{"not a service desk request", "/comment", http.StatusNotFound, jiraservicemanagement.GetTicketBranchNotFound},
		{"not an agent", "/sla", http.StatusForbidden, jiraservicemanagement.GetTicketBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				switch {
				case strings.HasSuffix(request.URL.Path, test.failing) || (test.failing == "issue" && strings.Contains(request.URL.Path, "/issue/")):
					writeJSON(t, response, test.status, `{"errorMessage":"SENTINEL denied"}`)
				case strings.HasSuffix(request.URL.Path, "/comment"):
					writeJSON(t, response, http.StatusOK, commentPageJSON)
				default:
					writeJSON(t, response, http.StatusOK, issueJSON("10042", "ITH-42", "Laptop will not boot", `[]`))
				}
			})
			result, err := sdkgo.RunQuery(newTestDexContext("get-"+test.name), newTestClient(t, provider.URL).GetTicket(), jsmConnection,
				jiraservicemanagement.GetTicketInput{IssueIDOrKey: "ITH-42"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			requireNoSentinel(t, result)
		})
	}
}

func TestGetTicketRejectsAnInvalidCommentPage(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if strings.HasSuffix(request.URL.Path, "/comment") {
			writeJSON(t, response, http.StatusOK, `{"isLastPage":true,"values":[{"id":"1000","body":"no visibility"}]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, issueJSON("10042", "ITH-42", "Laptop will not boot", `[]`))
	})
	result, err := sdkgo.RunQuery(newTestDexContext("get-invalid-comments"), newTestClient(t, provider.URL).GetTicket(), jsmConnection,
		jiraservicemanagement.GetTicketInput{IssueIDOrKey: "ITH-42"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.GetTicketBranchInvalidResponse, result.Branch, "a comment without its visibility could leak an internal note")
}
