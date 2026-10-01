// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

import (
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const createdTaskBody = `{"data":{"gid":"` + testTaskID + `","resource_type":"task","name":"[REQ-1042] Replace badge reader",` +
	`"permalink_url":"https://app.asana.com/0/` + testProjectID + `/` + testTaskID + `","created_at":"2026-09-30T16:15:00.000Z"}}`

func validCreateTaskInput() asana.CreateTaskInput {
	priority, budget := "1300000000000021", 1250.5
	text := "REQ-1042"
	return asana.CreateTaskInput{
		Name: "[REQ-1042] Replace badge reader", Notes: "Badge reader at door 4 is offline.\nApproved by Grace.",
		ProjectID: testProjectID, SectionID: testSectionID, AssigneeID: "ada@example.com", DueOn: "2026-10-15",
		CustomFields: []asana.CustomFieldValueInput{
			{CustomFieldID: "1300000000000001", Text: &text},
			{CustomFieldID: "1300000000000002", EnumOptionID: priority},
			{CustomFieldID: "1300000000000003", Number: &budget},
			{CustomFieldID: "1300000000000004", MultiEnumOptionIDs: []string{"1300000000000041", "1300000000000042"}},
		},
	}
}

func TestCreateTaskSendsTheSectionMembershipAndReturnsTheTaskID(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, createdTaskBody)
	})
	client := newAsanaClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newAsanaDexContext("create"), client.CreateTask(), asanaConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, asana.CreateTaskBranchCreated, result.Branch)
	createdAt := time.Date(2026, time.September, 30, 16, 15, 0, 0, time.UTC)
	require.Equal(t, asana.CreateTaskOutput{
		TaskID: testTaskID, Name: "[REQ-1042] Replace badge reader", ProjectID: testProjectID, SectionID: testSectionID,
		PermalinkURL: "https://app.asana.com/0/" + testProjectID + "/" + testTaskID, CreatedAt: &createdAt,
	}, result.Value)
	require.Equal(t, testTaskID, result.Receipt.ProviderObjectID)
	require.Equal(t, sdkgo.IdempotencyKey(result.Receipt.CallID), result.Receipt.IdempotencyKey)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/tasks", request.path)
	query, err := url.ParseQuery(request.rawQuery)
	require.NoError(t, err)
	require.Equal(t, "name,permalink_url,created_at", query.Get("opt_fields"))
	require.JSONEq(t, `{"data":{
		"name":"[REQ-1042] Replace badge reader","notes":"Badge reader at door 4 is offline.\nApproved by Grace.",
		"memberships":[{"project":"`+testProjectID+`","section":"`+testSectionID+`"}],
		"assignee":"ada@example.com","due_on":"2026-10-15",
		"custom_fields":{"1300000000000001":"REQ-1042","1300000000000002":"1300000000000021","1300000000000003":1250.5,
			"1300000000000004":["1300000000000041","1300000000000042"]}}}`, request.body)
}

func TestCreateTaskInAProjectOrWorkspaceOmitsBlankOptionalFields(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, createdTaskBody)
	})
	client := newAsanaClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newAsanaDexContext("create-project"), client.CreateTask(), asanaConnection,
		asana.CreateTaskInput{Name: "  Rotate badges  ", ProjectID: testProjectID, Notes: "   "})
	require.NoError(t, err)
	require.Equal(t, asana.CreateTaskBranchCreated, result.Branch)
	require.JSONEq(t, `{"data":{"name":"Rotate badges","projects":["`+testProjectID+`"]}}`, provider.request(0).body)

	result, err = sdkgo.RunMutation(newAsanaDexContext("create-workspace"), client.CreateTask(), asanaConnection,
		asana.CreateTaskInput{Name: "Call the locksmith", WorkspaceID: "1100000000000001", AssigneeID: "me"})
	require.NoError(t, err)
	require.Equal(t, asana.CreateTaskBranchCreated, result.Branch)
	require.JSONEq(t, `{"data":{"name":"Call the locksmith","workspace":"1100000000000001","assignee":"me"}}`, provider.request(1).body)
}

func TestCreateTaskNeverResendsARequestAsanaMayHaveReceived(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
		isRetry bool
	}{
		{name: "invalid assignee", status: http.StatusBadRequest, body: `{"errors":[{"message":"assignee: SENTINEL Not a recognized ID"}]}`, branch: asana.CreateTaskBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "unknown project", status: http.StatusNotFound, body: `{"errors":[{"message":"SENTINEL project: Unknown object"}]}`, branch: asana.CreateTaskBranchProviderRejected, kind: sdkgo.FailureNotFound},
		{name: "permission", status: http.StatusForbidden, body: `{"errors":[{"message":"SENTINEL Forbidden"}]}`, branch: asana.CreateTaskBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "premium field", status: http.StatusPaymentRequired, body: `{"errors":[{"message":"SENTINEL premium only"}]}`, branch: asana.CreateTaskBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "rate limit", status: http.StatusTooManyRequests, body: `{"errors":[{"message":"SENTINEL"}]}`, isRetry: true, kind: sdkgo.FailureRateLimit},
		{name: "server error", status: http.StatusInternalServerError, body: `{"errors":[{"message":"SENTINEL","phrase":"6 sad squid snuggle softly"}]}`, branch: asana.CreateTaskBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "unavailable", status: http.StatusServiceUnavailable, body: `{}`, branch: asana.CreateTaskBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, body: `{}`, branch: asana.CreateTaskBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "redirect", status: http.StatusFound, body: `{}`, branch: asana.CreateTaskBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "unusable created body", status: http.StatusCreated, body: `{"data":{"gid":"SENTINEL"}}`, branch: asana.CreateTaskBranchUncertain, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newAsanaClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newAsanaDexContext("create-"+test.name), client.CreateTask(), asanaConnection, validCreateTaskInput())
			require.Equal(t, 1, provider.requestCount())
			if test.isRetry {
				requireRetry(t, err, test.kind)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Empty(t, result.Value.TaskID)
			require.Equal(t, testProjectID, result.Value.ProjectID)
			require.Equal(t, "[REQ-1042] Replace badge reader", result.Value.Name)
			requireNoSentinel(t, result)
		})
	}
}

func TestCreateTaskTimeoutAfterDispatchIsUncertain(t *testing.T) {
	provider := newRecordingAsana(t, func(_ http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	client := newAsanaClient(t, provider.URL, asana.WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))
	result, err := sdkgo.RunMutation(newAsanaDexContext("create-timeout"), client.CreateTask(), asanaConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, asana.CreateTaskBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateTaskRefusedConnectionRetriesBecauseNothingWasSent(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedEndpoint := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	client := newAsanaClient(t, closedEndpoint)
	_, err = sdkgo.RunMutation(newAsanaDexContext("create-refused"), client.CreateTask(), asanaConnection, validCreateTaskInput())
	requireRetry(t, err, sdkgo.FailureTransport)
}

func TestCreateTaskRejectsInvalidInputWithoutARequest(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*asana.CreateTaskInput)
		message string
	}{
		{name: "no container", mutate: func(input *asana.CreateTaskInput) { input.ProjectID, input.SectionID = "", "" }, message: "set exactly one of projectId and workspaceId"},
		{name: "two containers", mutate: func(input *asana.CreateTaskInput) { input.WorkspaceID = "1100000000000001" }, message: "set exactly one of projectId and workspaceId"},
		{name: "section without project", mutate: func(input *asana.CreateTaskInput) { input.ProjectID, input.WorkspaceID = "", "1100000000000001" }, message: "sectionId requires projectId"},
		{name: "project name", mutate: func(input *asana.CreateTaskInput) { input.ProjectID = "Facilities" }, message: "projectId must be an Asana gid, a decimal string such as 1204567890123456"},
		{name: "blank name", mutate: func(input *asana.CreateTaskInput) { input.Name = " " }, message: "name is required"},
		{name: "multi-line name", mutate: func(input *asana.CreateTaskInput) { input.Name = "one\ntwo" }, message: "name must be one line without control characters"},
		{name: "assignee name", mutate: func(input *asana.CreateTaskInput) { input.AssigneeID = "Ada Lovelace" }, message: "assigneeId must be me, a user gid, or an email address"},
		{name: "due date format", mutate: func(input *asana.CreateTaskInput) { input.DueOn = "15/10/2026" }, message: "dueOn must be a YYYY-MM-DD date such as 2026-10-15"},
		{name: "control character", mutate: func(input *asana.CreateTaskInput) { input.Notes = "bell\a" }, message: "notes cannot contain control characters"},
		{name: "two custom field values", mutate: func(input *asana.CreateTaskInput) {
			input.CustomFields[0].EnumOptionID = "1300000000000021"
		}, message: "custom field 1300000000000001: set exactly one of text, number, enumOptionId, and multiEnumOptionIds"},
		{name: "repeated custom field", mutate: func(input *asana.CreateTaskInput) {
			input.CustomFields[1].CustomFieldID = input.CustomFields[0].CustomFieldID
		}, message: "custom field 1300000000000001 is set twice"},
		{name: "enum option name", mutate: func(input *asana.CreateTaskInput) {
			input.CustomFields[1].EnumOptionID = "High"
		}, message: "custom field 1300000000000002: enumOptionId must be an Asana gid, a decimal string such as 1204567890123456"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
			client := newAsanaClient(t, provider.URL)
			input := validCreateTaskInput()
			test.mutate(&input)
			result, err := sdkgo.RunMutation(newAsanaDexContext("create-invalid-"+test.name), client.CreateTask(), asanaConnection, input)
			require.NoError(t, err)
			require.Equal(t, asana.CreateTaskBranchDefect, result.Branch)
			require.Equal(t, test.message, result.Failure.Message)
		})
	}
}
