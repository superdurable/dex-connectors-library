//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package claimconversation

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeHiver is a stateful Hiver API v1 fake that, like Hiver, answers 429 to requests too close together.
type fakeHiver struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	enforcedInterval    time.Duration
	lastRequestAt       time.Time
	rateLimitViolations int

	inboxes            []*fakeInbox
	conversations      map[string]*fakeConversation
	conversationOrder  []string
	nextConversationID int64
	nextMessageID      int64
	nextNoteID         int64
	nextDraftID        int64
	counts             map[string]int
	requests           map[string][]fakeRecordedRequest

	claimsOnRead           string
	delaysFirstUpdate      bool
	delaysFirstNote        bool
	losesFirstNoteResponse bool
	rateLimitsFirstNote    bool
	holdsFirstDraft        chan struct{}
}

type fakeInbox struct {
	id    string
	email string
	users map[string]string
	tags  map[string]string
}

type fakeConversation struct {
	id         string
	inboxID    string
	status     string
	assigneeID string
	tagIDs     []string
	messageIDs []string
	notes      []fakeNote
	drafts     []fakeDraft
}

type fakeNote struct {
	id      string
	content string
}

type fakeDraft struct {
	id                    string
	body                  string
	replyToHiverMessageID string
}

type fakeRecordedRequest struct {
	at    time.Time
	query map[string][]string
	body  string
}

// newFakeHiver enforces 80 percent of the client's interval, so local timing jitter is not a violation.
func newFakeHiver(t *testing.T, clientInterval time.Duration) *fakeHiver {
	t.Helper()
	provider := &fakeHiver{
		t: t, enforcedInterval: clientInterval * 8 / 10, conversations: map[string]*fakeConversation{},
		nextConversationID: 573741000, nextMessageID: 834466000, nextNoteID: 35042600, nextDraftID: 900,
		counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeHiver) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+integrationAPIKey {
		provider.writeJSON(response, http.StatusUnauthorized, `{"Message":"Unauthorized"}`)
		return
	}
	if !provider.admitRequest() {
		provider.writeJSON(response, http.StatusTooManyRequests, `{"Message":"Too Many Requests"}`)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"Message":"unreadable body"}`)
		return
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1"), "/"), "/")
	route := request.Method + " " + strings.Join(segments[min(2, len(segments)):], "/")
	if segments[0] != "inboxes" {
		provider.writeJSON(response, http.StatusNotFound, `{"Message":"Not found"}`)
		return
	}
	if len(segments) == 1 && request.Method == http.MethodGet {
		provider.listInboxes(response, request, body)
		return
	}
	inbox := provider.inbox(segments[1])
	if inbox == nil {
		provider.writeJSON(response, http.StatusNotFound, `{"Message":"Inbox not found"}`)
		return
	}
	switch {
	case route == "GET conversations":
		provider.listConversations(response, request, body, inbox)
	case route == "POST conversations/shared-drafts":
		provider.createSharedDraft(response, request, body, inbox)
	case route == "GET users/search":
		provider.searchUsers(response, request, body, inbox)
	case route == "GET tags":
		provider.listTags(response, request, body, inbox)
	case len(segments) >= 4 && segments[2] == "conversations":
		conversation := provider.conversationIn(inbox, segments[3])
		if conversation == nil {
			provider.writeJSON(response, http.StatusNotFound, `{"Message":"Conversation not found"}`)
			return
		}
		switch {
		case len(segments) == 4 && request.Method == http.MethodGet:
			provider.readConversation(response, request, body, conversation)
		case len(segments) == 4 && request.Method == http.MethodPatch:
			provider.updateConversation(response, request, body, inbox, conversation)
		case len(segments) == 5 && segments[4] == "notes" && request.Method == http.MethodPost:
			provider.addNote(response, request, body, conversation)
		default:
			provider.writeJSON(response, http.StatusNotFound, `{"Message":"Not found"}`)
		}
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"Message":"Not found"}`)
	}
}

// admitRequest enforces the spacing and counts every violation.
func (provider *fakeHiver) admitRequest() bool {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	now := time.Now()
	isTooSoon := !provider.lastRequestAt.IsZero() && now.Sub(provider.lastRequestAt) < provider.enforcedInterval
	provider.lastRequestAt = now
	if isTooSoon {
		provider.rateLimitViolations++
	}
	return !isTooSoon
}

func (provider *fakeHiver) listInboxes(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("inboxes", request, body)
	provider.mutex.Lock()
	inboxes := make([]any, 0, len(provider.inboxes))
	for _, inbox := range provider.inboxes {
		inboxes = append(inboxes, map[string]any{
			"id": inbox.id, "display_name": "Inbox " + inbox.id, "channel_type": "email", "email": inbox.email, "inbox_type": "user",
			"is_authorised": true, "source_user": map[string]any{"id": "12323", "email": inbox.email}, "created_at": 1709036168, "updated_at": 1709036168,
		})
	}
	provider.mutex.Unlock()
	provider.writePage(response, request, inboxes)
}

func (provider *fakeHiver) listConversations(response http.ResponseWriter, request *http.Request, body []byte, inbox *fakeInbox) {
	provider.record("conversations", request, body)
	provider.mutex.Lock()
	var conversations []any
	for _, id := range provider.conversationOrder {
		if conversation := provider.conversations[id]; conversation.inboxID == inbox.id {
			value := provider.conversationJSON(conversation)
			delete(value, "message_ids")
			conversations = append(conversations, value)
		}
	}
	provider.mutex.Unlock()
	provider.writePage(response, request, conversations)
}

// readConversation answers with a one-element data array, as Hiver's documentation shows.
func (provider *fakeHiver) readConversation(response http.ResponseWriter, request *http.Request, body []byte, conversation *fakeConversation) {
	provider.record("read", request, body)
	provider.mutex.Lock()
	if provider.claimsOnRead == conversation.id {
		conversation.assigneeID = "456343"
	}
	value := provider.conversationJSON(conversation)
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, map[string]any{"data": []any{value}})
}

func (provider *fakeHiver) updateConversation(response http.ResponseWriter, request *http.Request, body []byte, inbox *fakeInbox, conversation *fakeConversation) {
	attempt := provider.record("update", request, body)
	if provider.delaysFirstUpdate && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var change struct {
		Status   *struct{ Name string }  `json:"status"`
		Assignee *struct{ Email string } `json:"assignee"`
		Tags     *struct {
			ToApply  []string `json:"to_apply"`
			ToRemove []string `json:"to_remove"`
		} `json:"tags"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &change))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	statuses := map[string]string{"open": "open", "pending": "pending", "close": "closed"}
	if change.Status != nil && statuses[change.Status.Name] == "" {
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"invalid status"}]}`)
		return
	}
	assigneeID := ""
	if change.Assignee != nil {
		for id, email := range inbox.users {
			if strings.EqualFold(email, change.Assignee.Email) {
				assigneeID = id
			}
		}
		if assigneeID == "" {
			provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"assignee is not an inbox user"}]}`)
			return
		}
	}
	if change.Tags != nil {
		for _, id := range slices.Concat(change.Tags.ToApply, change.Tags.ToRemove) {
			if _, isTag := inbox.tags[id]; !isTag {
				provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"unknown tag"}]}`)
				return
			}
		}
		for _, id := range change.Tags.ToApply {
			if !slices.Contains(conversation.tagIDs, id) {
				conversation.tagIDs = append(conversation.tagIDs, id)
			}
		}
		conversation.tagIDs = slices.DeleteFunc(conversation.tagIDs, func(id string) bool { return slices.Contains(change.Tags.ToRemove, id) })
	}
	if change.Status != nil {
		conversation.status = statuses[change.Status.Name]
	}
	if assigneeID != "" {
		conversation.assigneeID = assigneeID
	}
	response.WriteHeader(http.StatusNoContent)
}

func (provider *fakeHiver) addNote(response http.ResponseWriter, request *http.Request, body []byte, conversation *fakeConversation) {
	attempt := provider.record("note", request, body)
	if provider.rateLimitsFirstNote && attempt == 1 {
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"Message":"Too Many Requests"}`)
		return
	}
	if provider.delaysFirstNote && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	fields := provider.formFields(request, body)
	provider.mutex.Lock()
	provider.nextNoteID++
	note := fakeNote{id: strconv.FormatInt(provider.nextNoteID, 10), content: fields["content"]}
	conversation.notes = append(conversation.notes, note)
	provider.mutex.Unlock()
	if provider.losesFirstNoteResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusCreated, map[string]any{"data": map[string]any{
		"id": note.id, "conversation_id": conversation.id, "content": note.content,
		"author": map[string]any{"id": "540349", "email": "admin@acme.example.com"}, "mentions": []string{}, "parent_note_id": nil,
		"attachments": []any{}, "created_at": "2026-07-21T14:24:22.000000Z",
	}})
}

func (provider *fakeHiver) createSharedDraft(response http.ResponseWriter, request *http.Request, body []byte, inbox *fakeInbox) {
	attempt := provider.record("draft", request, body)
	if provider.holdsFirstDraft != nil && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		<-provider.holdsFirstDraft
	}
	fields := provider.formFields(request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for _, id := range provider.conversationOrder {
		conversation := provider.conversations[id]
		if conversation.inboxID != inbox.id || !slices.Contains(conversation.messageIDs, fields["hiver_message_id"]) || fields["body"] == "" {
			continue
		}
		provider.nextDraftID++
		draft := fakeDraft{id: strconv.FormatInt(provider.nextDraftID, 10), body: fields["body"], replyToHiverMessageID: fields["hiver_message_id"]}
		conversation.drafts = append(conversation.drafts, draft)
		conversationID, _ := strconv.ParseInt(conversation.id, 10, 64)
		messageID, _ := strconv.ParseInt(draft.replyToHiverMessageID, 10, 64)
		draftID, _ := strconv.ParseInt(draft.id, 10, 64)
		provider.writeValue(response, http.StatusOK, map[string]any{"data": map[string]any{
			"id": conversationID, "shared_draft_id": draftID, "reply_to_message_id": messageID,
		}})
		return
	}
	provider.writeJSON(response, http.StatusNotFound, `{"Message":"Message not found"}`)
}

func (provider *fakeHiver) searchUsers(response http.ResponseWriter, request *http.Request, body []byte, inbox *fakeInbox) {
	provider.record("users", request, body)
	email := request.URL.Query().Get("email")
	var users []any
	provider.mutex.Lock()
	for id, known := range inbox.users {
		if strings.EqualFold(known, email) {
			users = append(users, map[string]any{"id": id, "first_name": "Agent", "last_name": id, "email": known, "phone_number": "", "is_joined": true})
		}
	}
	provider.mutex.Unlock()
	provider.writePage(response, request, users)
}

func (provider *fakeHiver) listTags(response http.ResponseWriter, request *http.Request, body []byte, inbox *fakeInbox) {
	provider.record("tags", request, body)
	provider.mutex.Lock()
	ids := make([]string, 0, len(inbox.tags))
	for id := range inbox.tags {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	tags := make([]any, 0, len(ids))
	for _, id := range ids {
		tags = append(tags, map[string]any{"id": id, "name": inbox.tags[id], "color_code": "#ce93d8", "type": "user", "created_at": 1708945347})
	}
	provider.mutex.Unlock()
	provider.writePage(response, request, tags)
}

// seedStandardInbox adds support@acme.example.com with two agents and three tags.
func (provider *fakeHiver) seedStandardInbox() {
	provider.seedInbox(integrationInboxID, integrationInboxEmail)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	inbox := provider.inboxes[len(provider.inboxes)-1]
	inbox.users = map[string]string{"456342": integrationAssignee, "456343": "ben@acme.example.com"}
	inbox.tags = map[string]string{"56789": "Priority", "784268": integrationTagName, "784270": "VIP"}
}

func (provider *fakeHiver) seedInbox(id string, email string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.inboxes = append(provider.inboxes, &fakeInbox{id: id, email: email, users: map[string]string{}, tags: map[string]string{}})
}

// seedConversation adds a conversation with two messages and returns its ID.
func (provider *fakeHiver) seedConversation(inboxID string, status string, assigneeID string, tagIDs []string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextConversationID++
	provider.nextMessageID += 2
	conversation := &fakeConversation{
		id: strconv.FormatInt(provider.nextConversationID, 10), inboxID: inboxID, status: status, assigneeID: assigneeID,
		tagIDs:     append([]string{}, tagIDs...),
		messageIDs: []string{strconv.FormatInt(provider.nextMessageID-1, 10), strconv.FormatInt(provider.nextMessageID, 10)},
	}
	provider.conversations[conversation.id] = conversation
	provider.conversationOrder = append(provider.conversationOrder, conversation.id)
	return conversation.id
}

// conversationJSON requires provider.mutex; IDs are numbers, as in Hiver's list example.
func (provider *fakeHiver) conversationJSON(conversation *fakeConversation) map[string]any {
	id, _ := strconv.ParseInt(conversation.id, 10, 64)
	var assignee any
	if conversation.assigneeID != "" {
		assigneeID, _ := strconv.ParseInt(conversation.assigneeID, 10, 64)
		assignee = map[string]any{"assignee_type": "user", "assignee_id": assigneeID}
	}
	messages := make([]any, 0, len(conversation.messageIDs))
	for _, messageID := range conversation.messageIDs {
		number, _ := strconv.ParseInt(messageID, 10, 64)
		messages = append(messages, map[string]any{"hiver_message_id": number, "gmail_message_id": strconv.FormatInt(number, 16)})
	}
	return map[string]any{
		"id": id, "assignee": assignee, "status": conversation.status, "tag_ids": append([]string{}, conversation.tagIDs...),
		"gmail_thread_id": strconv.FormatInt(id, 16), "private_permalink": "https://v2.hiverhq.com/permalinks/pvt/" + conversation.id,
		"public_permalink": "https://v2.hiverhq.com/permalinks/pub/" + conversation.id, "message_ids": messages,
	}
}

func (provider *fakeHiver) inbox(id string) *fakeInbox {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for _, inbox := range provider.inboxes {
		if inbox.id == id {
			return inbox
		}
	}
	return nil
}

func (provider *fakeHiver) conversationIn(inbox *fakeInbox, id string) *fakeConversation {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if conversation := provider.conversations[id]; conversation != nil && conversation.inboxID == inbox.id {
		return conversation
	}
	return nil
}

// writePage pages items with Hiver's limit (10 to 100, default 10) and an opaque base64 next_page token.
func (provider *fakeHiver) writePage(response http.ResponseWriter, request *http.Request, items []any) {
	limit := 10
	if value := request.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 10 || parsed > 100 {
			provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"invalid limit"}]}`)
			return
		}
		limit = parsed
	}
	offset := 0
	if token := request.URL.Query().Get("next_page"); token != "" {
		decoded, err := base64.StdEncoding.DecodeString(token)
		if err != nil || !strings.HasPrefix(string(decoded), "offset:") {
			provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"invalid next_page"}]}`)
			return
		}
		offset, _ = strconv.Atoi(strings.TrimPrefix(string(decoded), "offset:"))
	}
	end := min(offset+limit, len(items))
	results := append([]any{}, items[min(offset, end):end]...)
	var nextPage any
	if end < len(items) {
		nextPage = base64.StdEncoding.EncodeToString([]byte("offset:" + strconv.Itoa(end)))
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"data": map[string]any{"results": results, "pagination": map[string]any{"next_page": nextPage}}})
}

func (provider *fakeHiver) formFields(request *http.Request, body []byte) map[string]string {
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	require.NoError(provider.t, err)
	require.Equal(provider.t, "multipart/form-data", mediaType)
	form, err := multipart.NewReader(strings.NewReader(string(body)), parameters["boundary"]).ReadForm(1 << 20)
	require.NoError(provider.t, err)
	fields := map[string]string{}
	for name, values := range form.Value {
		fields[name] = values[0]
	}
	return fields
}

func (provider *fakeHiver) record(name string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	provider.requests[name] = append(provider.requests[name], fakeRecordedRequest{at: time.Now(), query: request.URL.Query(), body: string(body)})
	return provider.counts[name]
}

func (provider *fakeHiver) writeValue(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	provider.writeJSON(response, status, string(encoded))
}

func (provider *fakeHiver) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake Hiver response write failed: %v", err)
	}
}

// dropConnection closes the connection after Hiver applied the request, as a lost response would.
func (provider *fakeHiver) dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(provider.t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(provider.t, err)
	require.NoError(provider.t, connection.Close())
}

func (provider *fakeHiver) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * slowResponseDelay):
		t.Fatal("a delayed fake Hiver request did not finish")
	}
}

func (provider *fakeHiver) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

// writeCount counts every request that could change Hiver: updates, notes, and drafts.
func (provider *fakeHiver) writeCount() int {
	return provider.count("update") + provider.count("note") + provider.count("draft")
}

func (provider *fakeHiver) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeHiver) requestTimes(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, request := range provider.requests[name] {
		times = append(times, request.at)
	}
	return times
}

func (provider *fakeHiver) rateLimitViolationCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.rateLimitViolations
}

func (provider *fakeHiver) conversation(id string) fakeConversation {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation := *provider.conversations[id]
	conversation.tagIDs = append([]string(nil), conversation.tagIDs...)
	conversation.notes = append([]fakeNote(nil), conversation.notes...)
	conversation.drafts = append([]fakeDraft(nil), conversation.drafts...)
	return conversation
}
