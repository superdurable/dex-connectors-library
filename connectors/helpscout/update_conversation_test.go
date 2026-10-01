// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// storedConversation is a fake conversation that PATCH and PUT change and GET returns.
type storedConversation struct {
	mu         sync.Mutex
	id         int64
	status     string
	assigneeID int64
	tags       []string
}

func (conversation *storedConversation) routes() map[string]http.HandlerFunc {
	path := "/v2/conversations/" + jsonNumber(conversation.id)
	return map[string]http.HandlerFunc{
		"GET " + path:   conversation.read,
		"PATCH " + path: conversation.patch,
		"PUT " + path + "/tags": func(response http.ResponseWriter, request *http.Request) {
			var body struct {
				Tags []string `json:"tags"`
			}
			if json.NewDecoder(request.Body).Decode(&body) != nil || body.Tags == nil {
				writeJSON(response, http.StatusBadRequest, helpScoutErrorBody("Invalid JSON", nil))
				return
			}
			conversation.mu.Lock()
			conversation.tags = body.Tags
			conversation.mu.Unlock()
			response.WriteHeader(http.StatusNoContent)
		},
	}
}

func (conversation *storedConversation) read(response http.ResponseWriter, _ *http.Request) {
	conversation.mu.Lock()
	defer conversation.mu.Unlock()
	body := conversationJSON(conversation.id, conversation.status, 123, conversation.tags...)
	if conversation.assigneeID == 0 {
		body = strings.Replace(body, `"assignee": {"id": 99, "type": "user", "first": "Mr", "last": "Robot", "email": "agent@acme.example.com"},`, ``, 1)
	} else {
		body = strings.Replace(body, `"assignee": {"id": 99,`, `"assignee": {"id": `+jsonNumber(conversation.assigneeID)+`,`, 1)
	}
	writeJSON(response, http.StatusOK, body)
}

func (conversation *storedConversation) patch(response http.ResponseWriter, request *http.Request) {
	var operation struct {
		Op    string          `json:"op"`
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value"`
	}
	if json.NewDecoder(request.Body).Decode(&operation) != nil {
		writeJSON(response, http.StatusBadRequest, helpScoutErrorBody("Invalid JSON", nil))
		return
	}
	conversation.mu.Lock()
	defer conversation.mu.Unlock()
	switch {
	case operation.Op == "replace" && operation.Path == "/status":
		if json.Unmarshal(operation.Value, &conversation.status) != nil {
			writeJSON(response, http.StatusBadRequest, helpScoutErrorBody("Bad request", map[string]string{"value": "EnumValue"}))
			return
		}
	case operation.Op == "replace" && operation.Path == "/assignTo":
		if json.Unmarshal(operation.Value, &conversation.assigneeID) != nil || conversation.assigneeID == 404 {
			writeJSON(response, http.StatusBadRequest, helpScoutErrorBody("Bad request", map[string]string{"value": "ValidConversationOwner"}))
			return
		}
	case operation.Op == "remove" && operation.Path == "/assignTo" && len(operation.Value) == 0:
		conversation.assigneeID = 0
	default:
		writeJSON(response, http.StatusBadRequest, helpScoutErrorBody("Allowed Patch Operations", nil))
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (conversation *storedConversation) snapshot() (string, int64, []string) {
	conversation.mu.Lock()
	defer conversation.mu.Unlock()
	return conversation.status, conversation.assigneeID, append([]string(nil), conversation.tags...)
}

func jsonNumber(value int64) string {
	encoded, _ := json.Marshal(value) // An int64 always encodes.
	return string(encoded)
}

func updateConversation(t *testing.T, client *helpscout.Client, input helpscout.UpdateConversationInput) (helpscout.UpdateConversationResult, error) {
	t.Helper()
	return sdkgo.RunMutation(newStepContext("update"), client.UpdateConversation(), testConnection, input)
}

func TestUpdateConversationWritesOnlyTheDifferencesAndReadsBack(t *testing.T) {
	stored := &storedConversation{id: 501, status: "active", assigneeID: 99, tags: []string{"VIP", "billing"}}
	fake := newFakeHelpScout(t, stored.routes())
	result, err := updateConversation(t, newFakeBackedClient(t, fake), helpscout.UpdateConversationInput{
		ConversationID: 501, Status: helpscout.ConversationStatusPending, AssigneeID: 77,
		AddTags: []string{"vip", "refund"}, RemoveTags: []string{"BILLING"},
	})
	require.NoError(t, err)
	require.Equal(t, helpscout.UpdateConversationBranchUpdated, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	status, assigneeID, tags := stored.snapshot()
	require.Equal(t, "pending", status)
	require.EqualValues(t, 77, assigneeID)
	require.Equal(t, []string{"VIP", "refund"}, tags, "tags compare without case and keep Help Scout's spelling")
	require.Equal(t, helpscout.ConversationStatusPending, result.Value.Conversation.Status, "the Result is the read-back")
	require.EqualValues(t, 77, result.Value.Conversation.Assignee.ID)

	patches := fake.requestsTo(http.MethodPatch, "/v2/conversations/501")
	require.Len(t, patches, 2)
	require.JSONEq(t, `{"op":"replace","path":"/status","value":"pending"}`, patches[0].body)
	require.JSONEq(t, `{"op":"replace","path":"/assignTo","value":77}`, patches[1].body)
	puts := fake.requestsTo(http.MethodPut, "/v2/conversations/501/tags")
	require.Len(t, puts, 1)
	require.JSONEq(t, `{"tags":["VIP","refund"]}`, puts[0].body)
	require.Len(t, fake.requestsTo(http.MethodGet, "/v2/conversations/501"), 2)

	repeated, err := updateConversation(t, newFakeBackedClient(t, fake), helpscout.UpdateConversationInput{
		ConversationID: 501, Status: helpscout.ConversationStatusPending, AssigneeID: 77,
		AddTags: []string{"vip", "refund"}, RemoveTags: []string{"BILLING"},
	})
	require.NoError(t, err)
	require.Equal(t, helpscout.UpdateConversationBranchUpdated, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyApplied, "a repeat finds every value applied")
	require.Len(t, fake.requestsTo(http.MethodPatch, "/v2/conversations/501"), 2, "and writes nothing")
	requireSecretFree(t, repeated)
}

func TestUpdateConversationUnassignsAndClearsEveryTag(t *testing.T) {
	stored := &storedConversation{id: 501, status: "closed", assigneeID: 99, tags: []string{"spam-check"}}
	fake := newFakeHelpScout(t, stored.routes())
	result, err := updateConversation(t, newFakeBackedClient(t, fake), helpscout.UpdateConversationInput{
		ConversationID: 501, IsUnassigned: true, RemoveTags: []string{"spam-check"},
	})
	require.NoError(t, err)
	require.Equal(t, helpscout.UpdateConversationBranchUpdated, result.Branch)
	require.Nil(t, result.Value.Conversation.Assignee)
	require.Empty(t, result.Value.Conversation.Tags)
	require.JSONEq(t, `{"op":"remove","path":"/assignTo"}`, fake.requestsTo(http.MethodPatch, "/v2/conversations/501")[0].body)
	require.JSONEq(t, `{"tags":[]}`, fake.requestsTo(http.MethodPut, "/v2/conversations/501/tags")[0].body)
}

func TestUpdateConversationMapsFailuresAndRetriesALostWrite(t *testing.T) {
	stored := &storedConversation{id: 501, status: "active", assigneeID: 99}
	routes := stored.routes()
	fake := newFakeHelpScout(t, routes)
	client := newFakeBackedClient(t, fake)

	rejected, err := updateConversation(t, client, helpscout.UpdateConversationInput{ConversationID: 501, AssigneeID: 404})
	require.NoError(t, err)
	require.Equal(t, helpscout.UpdateConversationBranchProviderRejected, rejected.Branch)
	require.Equal(t, sdkgo.FailureValidation, rejected.Failure.Kind)
	require.Contains(t, rejected.Failure.Message, "[value=ValidConversationOwner]")
	requireSecretFree(t, rejected)

	missing, err := updateConversation(t, client, helpscout.UpdateConversationInput{ConversationID: 404, Status: helpscout.ConversationStatusClosed})
	require.NoError(t, err)
	require.Equal(t, helpscout.UpdateConversationBranchNotFound, missing.Branch)

	lost := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations/501":   stored.read,
		"PATCH /v2/conversations/501": dropConnection,
	})
	_, err = updateConversation(t, newFakeBackedClient(t, lost), helpscout.UpdateConversationInput{ConversationID: 501, Status: helpscout.ConversationStatusClosed})
	requireRetry(t, err, sdkgo.FailureTransport)

	for _, input := range []helpscout.UpdateConversationInput{
		{Status: helpscout.ConversationStatusClosed},
		{ConversationID: 501},
		{ConversationID: 501, Status: helpscout.ConversationStatusAll},
		{ConversationID: 501, Status: "open"},
		{ConversationID: 501, AssigneeID: 7, IsUnassigned: true},
		{ConversationID: 501, AssigneeID: -7},
		{ConversationID: 501, AddTags: []string{"refund"}, RemoveTags: []string{"Refund"}},
		{ConversationID: 501, AddTags: []string{"a,b"}},
	} {
		result, err := updateConversation(t, client, input)
		require.NoError(t, err)
		require.Equal(t, helpscout.UpdateConversationBranchDefect, result.Branch, input)
	}
}
