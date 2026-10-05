//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package issuerequest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// integrationAPIKey is split so that no complete Linear key shape appears in source.
	integrationAPIKey = "lin_" + "api_" + "IntegrationKey0123456789abcdefghijklmnopq"
	integrationTeamID = "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"
	otherTeamID       = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
	aliceUserID       = "7c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f"
	backlogStateID    = "11111111-1111-4111-8111-111111111111"
	todoStateID       = "22222222-2222-4222-8222-222222222222"
	inProgressStateID = "33333333-3333-4333-8333-333333333333"
	doneStateID       = "44444444-4444-4444-8444-444444444444"
	// duplicateIDError is the answer this fake gives a repeated client ID; Linear's real code is undocumented.
	duplicateIDError = `{"errors":[{"message":"SENTINEL duplicate key value","extensions":{"type":"invalid input","code":"INPUT_ERROR","userError":true}}]}`
)

var fakeStates = map[string]struct{ name, stateType, teamID string }{
	backlogStateID: {"Backlog", "backlog", integrationTeamID}, todoStateID: {"Todo", "unstarted", integrationTeamID},
	inProgressStateID: {"In Progress", "started", integrationTeamID}, doneStateID: {"Done", "completed", integrationTeamID},
}

// fakeLinear is a stateful Linear GraphQL fake that stores client-supplied IDs and refuses to reuse one.
type fakeLinear struct {
	*httptest.Server
	t               *testing.T
	mutex           sync.Mutex
	delayedRequests sync.WaitGroup

	clock      time.Time
	nextNumber int
	issues     map[string]*fakeIssue
	comments   map[string]*fakeComment
	counts     map[string]int
	requests   map[string][]map[string]any

	delaysFirstCreate     bool
	delaysFirstComment    bool
	delaysFirstUpdate     bool
	rejectsCreate         bool
	rateLimitsFirstSearch bool
	holdsFirstCreate      chan struct{}
	slowResponseDelay     time.Duration
}

type fakeIssue struct {
	id, identifier, title, description, teamID, stateID, assigneeID string
	number                                                          int
	createdAt, updatedAt, seededAt                                  time.Time
}

type fakeComment struct {
	id, issueID, body string
	createdAt         time.Time
}

func newFakeLinear(t *testing.T) *fakeLinear {
	t.Helper()
	provider := &fakeLinear{
		t: t, clock: time.Date(2026, 1, 28, 9, 0, 0, 0, time.UTC), nextNumber: 100, issues: map[string]*fakeIssue{},
		comments: map[string]*fakeComment{}, counts: map[string]int{}, requests: map[string][]map[string]any{}, slowResponseDelay: slowResponseDelay,
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeLinear) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != integrationAPIKey {
		provider.writeJSON(response, http.StatusUnauthorized, `{"errors":[{"message":"Authentication required, not authenticated","extensions":{"type":"authentication error","code":"AUTHENTICATION_ERROR"}}]}`)
		return
	}
	var document struct {
		OperationName string         `json:"operationName"`
		Variables     map[string]any `json:"variables"`
	}
	body, err := io.ReadAll(request.Body)
	if request.Method != http.MethodPost || request.URL.Path != "/graphql" || err != nil || json.Unmarshal(body, &document) != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"bad request","extensions":{"code":"BAD_REQUEST"}}]}`)
		return
	}
	attempt := provider.record(document.OperationName, document.Variables)
	variables := document.Variables
	switch document.OperationName {
	case "LinearFindUserByEmail":
		provider.writeData(response, provider.findUser(variables))
	case "LinearSearchIssues":
		if provider.rateLimitsFirstSearch && attempt == 1 {
			response.Header().Set("Retry-After", "1")
			provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"SENTINEL rate limited","extensions":{"code":"RATELIMITED","type":"ratelimited"}}]}`)
			return
		}
		provider.writeData(response, map[string]any{"issues": map[string]any{
			"nodes": provider.matchingIssues(variables["filter"]), "pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
		}})
	case "LinearReadIssueSummary", "LinearGetIssue":
		provider.writeData(response, map[string]any{"issues": map[string]any{"nodes": provider.matchingIssues(variables["filter"])}})
	case "LinearCreateIssue":
		provider.createIssue(response, variables["input"].(map[string]any), attempt)
	case "LinearListWorkflowStates":
		provider.writeData(response, provider.workflowStates(fmt.Sprint(variables["teamId"])))
	case "LinearUpdateIssue":
		provider.updateIssue(response, fmt.Sprint(variables["id"]), variables["input"].(map[string]any), attempt)
	case "LinearAddComment":
		provider.addComment(response, variables["input"].(map[string]any), attempt)
	case "LinearReadComment":
		provider.writeData(response, provider.readComment(variables["filter"]))
	default:
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"x","extensions":{"code":"GRAPHQL_VALIDATION_FAILED","type":"graphql error"}}]}`)
	}
}

// delayResponse holds the answer of a write that Linear already applied, as a slow Linear would.
func (provider *fakeLinear) delayResponse(isDelayed bool) {
	if !isDelayed {
		return
	}
	provider.delayedRequests.Add(1)
	defer provider.delayedRequests.Done()
	if provider.holdsFirstCreate != nil {
		<-provider.holdsFirstCreate
		return
	}
	time.Sleep(provider.slowResponseDelay)
}

func (provider *fakeLinear) createIssue(response http.ResponseWriter, input map[string]any, attempt int) {
	if provider.rejectsCreate {
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"SENTINEL Entity not found: Team","extensions":{"type":"invalid input","code":"INPUT_ERROR","userError":true}}]}`)
		return
	}
	issueID := fmt.Sprint(input["id"])
	provider.mutex.Lock()
	if _, exists := provider.issues[issueID]; exists {
		provider.counts["duplicateCreate"]++
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusBadRequest, duplicateIDError)
		return
	}
	provider.counts["appliedCreate"]++
	provider.nextNumber++
	createdAt := provider.advanceClock()
	issue := &fakeIssue{
		id: issueID, number: provider.nextNumber, identifier: "ENG-" + strconv.Itoa(provider.nextNumber), title: fmt.Sprint(input["title"]),
		teamID: fmt.Sprint(input["teamId"]), stateID: backlogStateID, createdAt: createdAt, updatedAt: createdAt,
	}
	if description, hasDescription := input["description"]; hasDescription {
		issue.description = fmt.Sprint(description)
	}
	if assigneeID, hasAssignee := input["assigneeId"]; hasAssignee {
		issue.assigneeID = fmt.Sprint(assigneeID)
	}
	provider.issues[issueID] = issue
	answer := map[string]any{"issueCreate": map[string]any{"success": true, "issue": provider.issueJSON(issue)}}
	provider.mutex.Unlock()
	provider.delayResponse(attempt == 1 && (provider.delaysFirstCreate || provider.holdsFirstCreate != nil))
	provider.writeData(response, answer)
}

func (provider *fakeLinear) updateIssue(response http.ResponseWriter, issueID string, input map[string]any, attempt int) {
	provider.mutex.Lock()
	issue, isFound := provider.issues[issueID]
	if !isFound {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"SENTINEL Entity not found: Issue","extensions":{"type":"invalid input","code":"INPUT_ERROR"}}]}`)
		return
	}
	provider.counts["appliedUpdate"]++
	if stateID, hasState := input["stateId"]; hasState {
		issue.stateID = fmt.Sprint(stateID)
	}
	if assigneeID, hasAssignee := input["assigneeId"]; hasAssignee {
		issue.assigneeID = fmt.Sprint(assigneeID)
	}
	issue.updatedAt = provider.advanceClock()
	answer := map[string]any{"issueUpdate": map[string]any{"success": true, "issue": provider.issueJSON(issue)}}
	provider.mutex.Unlock()
	provider.delayResponse(attempt == 1 && provider.delaysFirstUpdate)
	provider.writeData(response, answer)
}

func (provider *fakeLinear) addComment(response http.ResponseWriter, input map[string]any, attempt int) {
	commentID := fmt.Sprint(input["id"])
	provider.mutex.Lock()
	if _, exists := provider.comments[commentID]; exists {
		provider.counts["duplicateComment"]++
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusBadRequest, duplicateIDError)
		return
	}
	if _, isFound := provider.issues[fmt.Sprint(input["issueId"])]; !isFound {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"SENTINEL Entity not found: Issue","extensions":{"type":"invalid input","code":"INPUT_ERROR"}}]}`)
		return
	}
	comment := &fakeComment{id: commentID, issueID: fmt.Sprint(input["issueId"]), body: fmt.Sprint(input["body"]), createdAt: provider.advanceClock()}
	provider.comments[commentID] = comment
	answer := map[string]any{"commentCreate": map[string]any{"success": true, "comment": provider.commentJSON(comment)}}
	provider.mutex.Unlock()
	provider.delayResponse(attempt == 1 && provider.delaysFirstComment)
	provider.writeData(response, answer)
}

func (provider *fakeLinear) findUser(variables map[string]any) map[string]any {
	nodes := []any{}
	if strings.EqualFold(fmt.Sprint(variables["email"]), "alice@example.com") {
		nodes = append(nodes, map[string]any{
			"id": aliceUserID, "name": "Alice Nguyen", "displayName": "alice", "email": "alice@example.com", "active": true,
			"admin": false, "guest": false, "url": "https://linear.app/acme/profiles/alice",
		})
	}
	return map[string]any{"users": map[string]any{"nodes": nodes}}
}

// matchingIssues applies the IssueFilter clauses the connector sends: id, team, number, title, and state type.
func (provider *fakeLinear) matchingIssues(rawFilter any) []any {
	filter, _ := rawFilter.(map[string]any)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	issues := make([]*fakeIssue, 0, len(provider.issues))
	for _, issue := range provider.issues {
		if provider.matchesFilter(issue, filter) {
			issues = append(issues, issue)
		}
	}
	sort.Slice(issues, func(left, right int) bool { return issues[left].number > issues[right].number })
	nodes := []any{}
	for _, issue := range issues {
		nodes = append(nodes, provider.issueJSON(issue))
	}
	return nodes
}

// matchesFilter requires provider.mutex.
func (provider *fakeLinear) matchesFilter(issue *fakeIssue, filter map[string]any) bool {
	equals := func(clause any, actual string) bool {
		comparator, _ := clause.(map[string]any)
		return comparator == nil || comparator["eq"] == nil || fmt.Sprint(comparator["eq"]) == actual
	}
	if !equals(filter["id"], issue.id) || !equals(filter["title"], issue.title) || !equals(filter["number"], strconv.Itoa(issue.number)) {
		return false
	}
	if team, hasTeam := filter["team"].(map[string]any); hasTeam && (!equals(team["id"], issue.teamID) || !equals(team["key"], provider.teamKey(issue.teamID))) {
		return false
	}
	if state, hasState := filter["state"].(map[string]any); hasState {
		typeClause, _ := state["type"].(map[string]any)
		types, _ := typeClause["in"].([]any)
		isOpen := false
		for _, stateType := range types {
			isOpen = isOpen || fmt.Sprint(stateType) == fakeStates[issue.stateID].stateType
		}
		if !isOpen {
			return false
		}
	}
	return true
}

func (provider *fakeLinear) teamKey(teamID string) string {
	if teamID == integrationTeamID {
		return "ENG"
	}
	return "OPS"
}

func (provider *fakeLinear) workflowStates(teamID string) map[string]any {
	nodes := []any{}
	position := 0.0
	for _, stateID := range []string{doneStateID, inProgressStateID, todoStateID, backlogStateID} {
		state := fakeStates[stateID]
		if state.teamID == teamID {
			position++
			nodes = append(nodes, map[string]any{"id": stateID, "name": state.name, "type": state.stateType, "position": position,
				"color": "#bec2c8", "team": map[string]any{"id": teamID}})
		}
	}
	return map[string]any{"workflowStates": map[string]any{"nodes": nodes}}
}

func (provider *fakeLinear) readComment(rawFilter any) map[string]any {
	commentID := fmt.Sprint(rawFilter.(map[string]any)["id"].(map[string]any)["eq"])
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	nodes := []any{}
	if comment, isFound := provider.comments[commentID]; isFound {
		nodes = append(nodes, provider.commentJSON(comment))
	}
	return map[string]any{"comments": map[string]any{"nodes": nodes}}
}

// issueJSON requires provider.mutex; it carries every field getIssue selects, so one shape serves both reads.
func (provider *fakeLinear) issueJSON(issue *fakeIssue) map[string]any {
	state := fakeStates[issue.stateID]
	var assignee any
	if issue.assigneeID != "" {
		assignee = map[string]any{"id": issue.assigneeID, "name": "Alice Nguyen", "displayName": "alice"}
	}
	return map[string]any{
		"id": issue.id, "identifier": issue.identifier, "number": issue.number, "title": issue.title,
		"url": "https://linear.app/acme/issue/" + issue.identifier, "priority": 0, "priorityLabel": "No priority", "estimate": nil,
		"dueDate": nil, "labelIds": []any{}, "branchName": "eng-" + strconv.Itoa(issue.number), "trashed": false, "description": issue.description,
		"createdAt": issue.createdAt.Format(time.RFC3339Nano), "updatedAt": issue.updatedAt.Format(time.RFC3339Nano), "archivedAt": nil,
		"startedAt": nil, "completedAt": nil, "canceledAt": nil,
		"team":     map[string]any{"id": issue.teamID, "key": provider.teamKey(issue.teamID), "name": "Engineering"},
		"state":    map[string]any{"id": issue.stateID, "name": state.name, "type": state.stateType},
		"assignee": assignee, "creator": nil, "labels": map[string]any{"nodes": []any{}}, "project": nil, "cycle": nil, "parent": nil,
	}
}

func (provider *fakeLinear) commentJSON(comment *fakeComment) map[string]any {
	return map[string]any{"id": comment.id, "url": "https://linear.app/acme/comment/" + comment.id, "createdAt": comment.createdAt.Format(time.RFC3339Nano)}
}

// seedIssue stores an issue as if a person had created it earlier.
func (provider *fakeLinear) seedIssue(teamID string, title string, stateID string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextNumber++
	seededAt := provider.advanceClock()
	issueID := fmt.Sprintf("aaaaaaaa-aaaa-4aaa-8aaa-%012d", provider.nextNumber)
	provider.issues[issueID] = &fakeIssue{
		id: issueID, number: provider.nextNumber, identifier: provider.teamKey(teamID) + "-" + strconv.Itoa(provider.nextNumber), title: title,
		teamID: teamID, stateID: stateID, createdAt: seededAt, updatedAt: seededAt, seededAt: seededAt,
	}
	return issueID
}

// advanceClock requires provider.mutex; every write moves updatedAt forward by one minute.
func (provider *fakeLinear) advanceClock() time.Time {
	provider.clock = provider.clock.Add(time.Minute)
	return provider.clock
}

func (provider *fakeLinear) record(operationName string, variables map[string]any) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[operationName]++
	provider.requests[operationName] = append(provider.requests[operationName], variables)
	return provider.counts[operationName]
}

func (provider *fakeLinear) writeData(response http.ResponseWriter, data any) {
	encoded, err := json.Marshal(map[string]any{"data": data})
	require.NoError(provider.t, err)
	provider.writeJSON(response, http.StatusOK, string(encoded))
}

func (provider *fakeLinear) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake Linear response write failed: %v", err)
	}
}

func (provider *fakeLinear) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(3 * slowResponseDelay):
		t.Fatal("a delayed fake Linear request did not finish")
	}
}

func (provider *fakeLinear) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeLinear) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeLinear) issueCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.issues)
}

func (provider *fakeLinear) issue(issueID string) fakeIssue {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return *provider.issues[issueID]
}

func (provider *fakeLinear) commentsOn(issueID string) []fakeComment {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	comments := []fakeComment{}
	for _, comment := range provider.comments {
		if comment.issueID == issueID {
			comments = append(comments, *comment)
		}
	}
	return comments
}

// distinctInputIDs returns the client-supplied IDs that every request of operationName sent.
func (provider *fakeLinear) distinctInputIDs(operationName string) map[string]bool {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	identifiers := map[string]bool{}
	for _, variables := range provider.requests[operationName] {
		identifiers[fmt.Sprint(variables["input"].(map[string]any)["id"])] = true
	}
	return identifiers
}

func (provider *fakeLinear) lastVariables(operationName string) map[string]any {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := provider.requests[operationName]
	require.NotEmpty(provider.t, requests, operationName)
	return requests[len(requests)-1]
}
