// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAccessToken = "directory-token"
	// providerMessageSentinel appears only in fake provider error text, never in a Failure.
	providerMessageSentinel = "SENTINEL provider detail"
)

var testConnection = sdkgo.ConnectionRef{Provider: "google", Name: "google-workspace-admin-test"}

type recordedRequest struct {
	method string
	path   string
	query  url.Values
	body   map[string]any
}

// fakeDirectory is a stateful Admin SDK Directory API fake keyed by lowercase address.
// intercept, when set, answers a request before the stateful behavior runs.
type fakeDirectory struct {
	*httptest.Server
	mutex     sync.Mutex
	users     map[string]map[string]any
	groups    map[string]map[string]map[string]any
	nextID    int
	requests  []recordedRequest
	intercept func(request recordedRequest, index int) (status int, body any, handled bool)
	// retryAfter, when set, is sent as a Retry-After header on every response.
	retryAfter string
}

func newFakeDirectory(t *testing.T) *fakeDirectory {
	t.Helper()
	directory := &fakeDirectory{users: map[string]map[string]any{}, groups: map[string]map[string]map[string]any{}, nextID: 100000000000000000}
	directory.Server = httptest.NewServer(http.HandlerFunc(directory.serveHTTP))
	t.Cleanup(directory.Close)
	return directory
}

func (directory *fakeDirectory) serveHTTP(response http.ResponseWriter, request *http.Request) {
	directory.mutex.Lock()
	defer directory.mutex.Unlock()
	if directory.retryAfter != "" {
		response.Header().Set("Retry-After", directory.retryAfter)
	}
	if request.Header.Get("Authorization") != "Bearer "+testAccessToken {
		writeFakeJSON(response, http.StatusUnauthorized, googleError(http.StatusUnauthorized, "authError"))
		return
	}
	contents, err := io.ReadAll(request.Body)
	if err != nil {
		writeFakeJSON(response, http.StatusBadRequest, googleError(http.StatusBadRequest, "badRequest"))
		return
	}
	recorded := recordedRequest{method: request.Method, path: request.URL.Path, query: request.URL.Query()}
	if len(contents) != 0 && json.Unmarshal(contents, &recorded.body) != nil {
		writeFakeJSON(response, http.StatusBadRequest, googleError(http.StatusBadRequest, "badRequest"))
		return
	}
	directory.requests = append(directory.requests, recorded)
	if directory.intercept != nil {
		if status, body, handled := directory.intercept(recorded, len(directory.requests)-1); handled {
			writeFakeJSON(response, status, body)
			return
		}
	}
	status, body := directory.answer(recorded)
	writeFakeJSON(response, status, body)
}

func (directory *fakeDirectory) answer(request recordedRequest) (int, any) {
	segments := strings.Split(strings.TrimPrefix(request.path, "/admin/directory/v1/"), "/")
	switch {
	case request.method == http.MethodPost && request.path == "/admin/directory/v1/users":
		email := strings.ToLower(request.body["primaryEmail"].(string))
		if _, exists := directory.users[email]; exists {
			return http.StatusConflict, googleError(http.StatusConflict, "duplicate")
		}
		directory.nextID++
		user := map[string]any{
			"kind": "admin#directory#user", "id": strconv.Itoa(directory.nextID), "primaryEmail": request.body["primaryEmail"],
			"name": request.body["name"], "orgUnitPath": request.body["orgUnitPath"], "suspended": false,
			"changePasswordAtNextLogin": request.body["changePasswordAtNextLogin"], "externalIds": request.body["externalIds"],
			"customerId": "C03az79cb", "creationTime": "2026-10-01T09:00:00.000Z", "isMailboxSetup": false,
		}
		directory.users[email] = user
		return http.StatusOK, user
	case request.method == http.MethodGet && request.path == "/admin/directory/v1/users":
		users := []any{}
		for _, user := range directory.users {
			users = append(users, user)
		}
		return http.StatusOK, map[string]any{"kind": "admin#directory#users", "users": users}
	case len(segments) == 2 && segments[0] == "users":
		user := directory.findUser(segments[1])
		if user == nil {
			return http.StatusNotFound, googleError(http.StatusNotFound, "notFound")
		}
		if request.method == http.MethodPut {
			if suspended, ok := request.body["suspended"].(bool); ok {
				user["suspended"] = suspended
				if suspended {
					user["suspensionReason"] = "ADMIN"
				} else {
					delete(user, "suspensionReason")
				}
			}
		}
		return http.StatusOK, user
	case len(segments) >= 3 && segments[0] == "groups" && segments[2] == "members":
		return directory.answerMembers(request, segments)
	default:
		return http.StatusNotFound, googleError(http.StatusNotFound, "notFound")
	}
}

func (directory *fakeDirectory) answerMembers(request recordedRequest, segments []string) (int, any) {
	members, groupExists := directory.groups[strings.ToLower(segments[1])]
	if !groupExists {
		return http.StatusNotFound, googleError(http.StatusNotFound, "notFound")
	}
	switch {
	case len(segments) == 3 && request.method == http.MethodPost:
		email := strings.ToLower(request.body["email"].(string))
		user := directory.findUser(email)
		if user == nil {
			return http.StatusNotFound, googleError(http.StatusNotFound, "notFound")
		}
		if _, exists := members[email]; exists {
			return http.StatusConflict, googleError(http.StatusConflict, "duplicate")
		}
		member := map[string]any{"kind": "admin#directory#member", "id": user["id"], "email": user["primaryEmail"], "role": request.body["role"], "type": "USER", "status": "ACTIVE"}
		members[email] = member
		return http.StatusOK, member
	case len(segments) == 3 && request.method == http.MethodGet:
		return http.StatusOK, map[string]any{"kind": "admin#directory#members"}
	case len(segments) == 4 && request.method == http.MethodGet:
		member, exists := members[strings.ToLower(segments[3])]
		if !exists {
			return http.StatusNotFound, googleError(http.StatusNotFound, "notFound")
		}
		return http.StatusOK, member
	case len(segments) == 4 && request.method == http.MethodDelete:
		if _, exists := members[strings.ToLower(segments[3])]; !exists {
			return http.StatusNotFound, googleError(http.StatusNotFound, "notFound")
		}
		delete(members, strings.ToLower(segments[3]))
		return http.StatusNoContent, nil
	default:
		return http.StatusNotFound, googleError(http.StatusNotFound, "notFound")
	}
}

func (directory *fakeDirectory) findUser(userKey string) map[string]any {
	if user, exists := directory.users[strings.ToLower(userKey)]; exists {
		return user
	}
	for _, user := range directory.users {
		if user["id"] == userKey {
			return user
		}
	}
	return nil
}

func (directory *fakeDirectory) seedUser(email string, externalIDs []any) map[string]any {
	directory.mutex.Lock()
	defer directory.mutex.Unlock()
	directory.nextID++
	user := map[string]any{
		"id": strconv.Itoa(directory.nextID), "primaryEmail": email, "name": map[string]any{"givenName": "Existing", "familyName": "Person"},
		"orgUnitPath": "/", "suspended": false, "externalIds": externalIDs, "recoveryEmail": "private@personal.example", "recoveryPhone": "+16505550100",
	}
	directory.users[strings.ToLower(email)] = user
	return user
}

func (directory *fakeDirectory) seedGroup(email string) {
	directory.mutex.Lock()
	defer directory.mutex.Unlock()
	directory.groups[strings.ToLower(email)] = map[string]map[string]any{}
}

func (directory *fakeDirectory) seedMember(groupEmail string, memberEmail string, role string) {
	directory.mutex.Lock()
	defer directory.mutex.Unlock()
	user := directory.findUser(memberEmail)
	directory.groups[strings.ToLower(groupEmail)][strings.ToLower(memberEmail)] = map[string]any{
		"id": user["id"], "email": memberEmail, "role": role, "type": "USER", "status": "ACTIVE",
	}
}

func (directory *fakeDirectory) setIntercept(intercept func(request recordedRequest, index int) (int, any, bool)) {
	directory.mutex.Lock()
	defer directory.mutex.Unlock()
	directory.intercept = intercept
}

func (directory *fakeDirectory) recorded() []recordedRequest {
	directory.mutex.Lock()
	defer directory.mutex.Unlock()
	return append([]recordedRequest(nil), directory.requests...)
}

func (directory *fakeDirectory) userCount() int {
	directory.mutex.Lock()
	defer directory.mutex.Unlock()
	return len(directory.users)
}

func (directory *fakeDirectory) memberCount(groupEmail string) int {
	directory.mutex.Lock()
	defer directory.mutex.Unlock()
	return len(directory.groups[strings.ToLower(groupEmail)])
}

func googleError(code int, reason string) map[string]any {
	return map[string]any{"error": map[string]any{
		"code": code, "message": providerMessageSentinel,
		"errors": []any{map[string]any{"domain": "global", "reason": reason, "message": providerMessageSentinel}},
	}}
}

func writeFakeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Goog-Request-Id", "google-request")
	response.WriteHeader(status)
	if body == nil {
		return
	}
	contents, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	if _, err := response.Write(contents); err != nil {
		panic(err)
	}
}

func staticCredentials() sdkgo.StaticCredentialProvider[workspaceadmin.Credentials] {
	return sdkgo.StaticCredentialProvider[workspaceadmin.Credentials]{
		testConnection: {AuthMethodID: workspaceadmin.WorkspaceDomainDelegationAuthMethodID, AccessToken: sdkgo.NewSecretString(testAccessToken)},
	}
}

func newTestClient(t *testing.T, endpoint string) *workspaceadmin.Client {
	t.Helper()
	client, err := workspaceadmin.New(workspaceadmin.Config{Endpoint: endpoint}, staticCredentials())
	require.NoError(t, err)
	return client
}

// requireRetry asserts that an operation asked Dex to retry and returns the safe Failure.
func requireRetry(t *testing.T, err error) sdkgo.Failure {
	t.Helper()
	var retry *sdkgo.RetryError
	require.True(t, errors.As(err, &retry), "expected a connector Retry, got %v", err)
	require.NotContains(t, retry.Error(), providerMessageSentinel)
	return retry.Failure
}

// requireSafeFailure asserts that a Failure carries no provider message text.
func requireSafeFailure(t *testing.T, failure *sdkgo.Failure) {
	t.Helper()
	require.NotNil(t, failure)
	require.NotContains(t, failure.Message, providerMessageSentinel)
}

type testDexContext struct {
	context.Context
	step           string
	firstAttemptAt time.Time
}

func newTestDexContext(step string) *testDexContext {
	return &testDexContext{Context: context.Background(), step: step, firstAttemptAt: time.Now()}
}
func (*testDexContext) FlowID() string                                  { return "workspace-admin-flow" }
func (*testDexContext) RunID() string                                   { return "run" }
func (*testDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *testDexContext) StepExecutionID() string                 { return context.step }
func (*testDexContext) FromStepExecutionID() string                     { return "" }
func (*testDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (context *testDexContext) FirstAttemptAt() time.Time               { return context.firstAttemptAt }
func (*testDexContext) Attempt() int32                                  { return 1 }
func (*testDexContext) HasTimerFired() bool                             { return false }
func (*testDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*testDexContext) WaitForMethodFailed() bool                       { return false }
func (*testDexContext) RecordHeartbeat(any) error                       { return nil }
func (*testDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*testDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*testDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*testDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*testDexContext)(nil)
