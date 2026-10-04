// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front_test

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// statefulConversation is a fake Front conversation that applies PATCH and tag writes.
type statefulConversation struct {
	t            *testing.T
	mutex        sync.Mutex
	conversation testConversation
	writeStatus  int
	// failingWriteMethod limits writeStatus to one write method; empty fails every write.
	failingWriteMethod string
}

func (state *statefulConversation) respond(response http.ResponseWriter, request *http.Request, _ int) {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	isFailingWrite := state.failingWriteMethod == "" || state.failingWriteMethod == request.Method
	if request.Method != http.MethodGet && state.writeStatus != 0 && isFailingWrite {
		writeFrontError(state.t, response, state.writeStatus)
		return
	}
	var body struct {
		Status     string   `json:"status"`
		StatusID   string   `json:"status_id"`
		AssigneeID *string  `json:"assignee_id"`
		TagIDs     []string `json:"tag_ids"`
	}
	fields := map[string]json.RawMessage{}
	if request.Method != http.MethodGet {
		encoded, err := io.ReadAll(request.Body)
		require.NoError(state.t, err)
		require.NoError(state.t, json.Unmarshal(encoded, &body))
		require.NoError(state.t, json.Unmarshal(encoded, &fields))
	}
	switch request.Method {
	case http.MethodPatch:
		switch body.Status {
		case "open":
			state.conversation.status = "unassigned"
		case "archived", "deleted":
			state.conversation.status = body.Status
		}
		if body.StatusID != "" {
			state.conversation.statusID = body.StatusID
		}
		if _, hasAssignee := fields["assignee_id"]; hasAssignee {
			state.conversation.assigneeID = ""
			if body.AssigneeID != nil {
				state.conversation.assigneeID = *body.AssigneeID
			}
		}
		if state.conversation.status == "unassigned" && state.conversation.assigneeID != "" {
			state.conversation.status = "assigned"
		}
	case http.MethodPost:
		state.conversation.tagIDs = append(state.conversation.tagIDs, body.TagIDs...)
	case http.MethodDelete:
		state.conversation.tagIDs = slices.DeleteFunc(state.conversation.tagIDs, func(tagID string) bool { return slices.Contains(body.TagIDs, tagID) })
	default:
		writeJSON(state.t, response, http.StatusOK, conversationJSON(state.t, state.conversation))
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func TestUpdateConversationWritesOnlyTheMissingValuesAndReadsBack(t *testing.T) {
	state := &statefulConversation{t: t, conversation: testConversation{id: testConversationID, status: "archived", tagIDs: []string{"tag_old", testTagID}}}
	provider := newRecordingFront(t, state.respond)
	result, err := sdkgo.RunMutation(newTestDexContext("update"), newFrontClient(t, provider.URL).UpdateConversation(), frontConnection,
		front.UpdateConversationInput{
			ConversationID: testConversationID, Status: front.ConversationStatusChangeOpen, AssigneeID: testTeammateID,
			AddTagIDs: []string{testTagID, "tag_new", "tag_new"}, RemoveTagIDs: []string{"tag_old", "tag_absent"},
		})
	require.NoError(t, err)
	require.Equal(t, front.UpdateConversationBranchUpdated, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, front.ConversationStatusAssigned, result.Value.Conversation.Status)
	require.Equal(t, testTeammateID, result.Value.Conversation.Assignee.ID)
	require.Equal(t, []string{testTagID, "tag_new"}, tagIDs(result.Value.Conversation))
	require.Equal(t, 5, provider.requestCount())
	require.Equal(t, http.MethodPatch, provider.request(1).method)
	require.JSONEq(t, `{"status":"open","assignee_id":"`+testTeammateID+`"}`, provider.request(1).body)
	require.Equal(t, "/conversations/"+testConversationID+"/tags", provider.request(2).path)
	require.JSONEq(t, `{"tag_ids":["tag_new"]}`, provider.request(2).body)
	require.Equal(t, http.MethodDelete, provider.request(3).method)
	require.JSONEq(t, `{"tag_ids":["tag_old"]}`, provider.request(3).body)
	require.Equal(t, http.MethodGet, provider.request(4).method)

	repeated, err := sdkgo.RunMutation(newTestDexContext("update-again"), newFrontClient(t, provider.URL).UpdateConversation(), frontConnection,
		front.UpdateConversationInput{
			ConversationID: testConversationID, Status: front.ConversationStatusChangeOpen, AssigneeID: testTeammateID,
			AddTagIDs: []string{testTagID, "tag_new"}, RemoveTagIDs: []string{"tag_old"},
		})
	require.NoError(t, err)
	require.Equal(t, front.UpdateConversationBranchUpdated, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyApplied, "a repeated attempt finds every value applied")
	require.Equal(t, 6, provider.requestCount(), "a repeated attempt only reads")
}

func TestUpdateConversationUnassignsWithANullAssignee(t *testing.T) {
	state := &statefulConversation{t: t, conversation: testConversation{id: testConversationID, status: "assigned", assigneeID: testTeammateID}}
	provider := newRecordingFront(t, state.respond)
	result, err := sdkgo.RunMutation(newTestDexContext("unassign"), newFrontClient(t, provider.URL).UpdateConversation(), frontConnection,
		front.UpdateConversationInput{ConversationID: testConversationID, IsUnassigned: true, Status: front.ConversationStatusChangeArchived})
	require.NoError(t, err)
	require.Equal(t, front.UpdateConversationBranchUpdated, result.Branch)
	require.JSONEq(t, `{"status":"archived","assignee_id":null}`, provider.request(1).body)
	require.Nil(t, result.Value.Conversation.Assignee)
}

func TestUpdateConversationRetriesUntilFrontShowsTheChange(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation{id: testConversationID, status: "archived"}))
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	_, err := sdkgo.RunMutation(newTestDexContext("update-lagging"), newFrontClient(t, provider.URL).UpdateConversation(), frontConnection,
		front.UpdateConversationInput{ConversationID: testConversationID, Status: front.ConversationStatusChangeOpen})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}

func TestUpdateConversationWritesATicketStatusAndReadsItBack(t *testing.T) {
	state := &statefulConversation{t: t, conversation: testConversation{id: testConversationID, status: "unassigned", statusID: "sts_1a"}}
	provider := newRecordingFront(t, state.respond)
	input := front.UpdateConversationInput{ConversationID: testConversationID, StatusID: "sts_5x"}
	result, err := sdkgo.RunMutation(newTestDexContext("update-status-id"), newFrontClient(t, provider.URL).UpdateConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.UpdateConversationBranchUpdated, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, "sts_5x", result.Value.Conversation.StatusID)
	require.Equal(t, http.MethodPatch, provider.request(1).method)
	require.JSONEq(t, `{"status_id":"sts_5x"}`, provider.request(1).body)
	require.Equal(t, 3, provider.requestCount())

	repeated, err := sdkgo.RunMutation(newTestDexContext("update-status-id-again"), newFrontClient(t, provider.URL).UpdateConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.UpdateConversationBranchUpdated, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyApplied)
	require.Equal(t, 4, provider.requestCount(), "a repeated attempt only reads")
}

func TestUpdateConversationReportsWritesAppliedBeforeALaterWriteFails(t *testing.T) {
	for status, branch := range map[int]sdkgo.BranchID{
		http.StatusNotFound:         front.UpdateConversationBranchProviderRejected,
		http.StatusMovedPermanently: front.UpdateConversationBranchNotFound,
	} {
		state := &statefulConversation{
			t: t, conversation: testConversation{id: testConversationID, status: "archived"}, writeStatus: status, failingWriteMethod: http.MethodPost,
		}
		provider := newRecordingFront(t, state.respond)
		result, err := sdkgo.RunMutation(newTestDexContext("update-later-write"), newFrontClient(t, provider.URL).UpdateConversation(), frontConnection,
			front.UpdateConversationInput{ConversationID: testConversationID, AssigneeID: testTeammateID, AddTagIDs: []string{testTagID}})
		require.NoError(t, err, status)
		require.Equal(t, branch, result.Branch, status)
		require.Contains(t, result.Failure.Message, "writes sent before it stay applied", status)
		require.Equal(t, testTeammateID, state.conversation.assigneeID, "the PATCH before the failed tag write stays applied")
	}
}

func TestUpdateConversationMapsWriteFailures(t *testing.T) {
	for status, branch := range map[int]sdkgo.BranchID{
		http.StatusBadRequest: front.UpdateConversationBranchProviderRejected, http.StatusNotFound: front.UpdateConversationBranchProviderRejected,
		http.StatusMovedPermanently: front.UpdateConversationBranchNotFound,
	} {
		state := &statefulConversation{t: t, conversation: testConversation{id: testConversationID, status: "archived"}, writeStatus: status}
		provider := newRecordingFront(t, state.respond)
		result, err := sdkgo.RunMutation(newTestDexContext("update-failed"), newFrontClient(t, provider.URL).UpdateConversation(), frontConnection,
			front.UpdateConversationInput{ConversationID: testConversationID, AddTagIDs: []string{testTagID}})
		require.NoError(t, err, status)
		require.Equal(t, branch, result.Branch, status)
		require.NotContains(t, result.Failure.Message, providerSentinel)
		require.NotContains(t, result.Failure.Message, "stay applied", "the first write has no earlier write")
	}
	missing := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeFrontError(t, response, http.StatusNotFound)
	})
	result, err := sdkgo.RunMutation(newTestDexContext("update-missing"), newFrontClient(t, missing.URL).UpdateConversation(), frontConnection,
		front.UpdateConversationInput{ConversationID: testConversationID, AddTagIDs: []string{testTagID}})
	require.NoError(t, err)
	require.Equal(t, front.UpdateConversationBranchNotFound, result.Branch, "a 404 on the read means the conversation")
	state := &statefulConversation{t: t, conversation: testConversation{id: testConversationID, status: "archived"}, writeStatus: http.StatusServiceUnavailable}
	provider := newRecordingFront(t, state.respond)
	_, err = sdkgo.RunMutation(newTestDexContext("update-unavailable"), newFrontClient(t, provider.URL).UpdateConversation(), frontConnection,
		front.UpdateConversationInput{ConversationID: testConversationID, AddTagIDs: []string{testTagID}})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "repeating an idempotent write after a server error is safe")
}

func TestUpdateConversationValidatesInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingFront(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	client := newFrontClient(t, provider.URL)
	for name, input := range map[string]front.UpdateConversationInput{
		"no change":             {ConversationID: testConversationID},
		"spam":                  {ConversationID: testConversationID, Status: "spam"},
		"reported status":       {ConversationID: testConversationID, Status: "assigned"},
		"status and status ID":  {ConversationID: testConversationID, Status: front.ConversationStatusChangeOpen, StatusID: "sts_5x"},
		"status name":           {ConversationID: testConversationID, StatusID: "Waiting on customer"},
		"assign and unassign":   {ConversationID: testConversationID, AssigneeID: testTeammateID, IsUnassigned: true},
		"tag name":              {ConversationID: testConversationID, AddTagIDs: []string{"billing"}},
		"added and removed tag": {ConversationID: testConversationID, AddTagIDs: []string{testTagID}, RemoveTagIDs: []string{testTagID}},
		"too many tags":         {ConversationID: testConversationID, AddTagIDs: make([]string, front.MaxTagChanges+1)},
	} {
		result, err := sdkgo.RunMutation(newTestDexContext("update-"+name), client.UpdateConversation(), frontConnection, input)
		require.NoError(t, err)
		require.Equal(t, front.UpdateConversationBranchDefect, result.Branch, name)
	}
}

func tagIDs(conversation front.Conversation) []string {
	ids := []string{}
	for _, tag := range conversation.Tags {
		ids = append(ids, tag.ID)
	}
	return ids
}
