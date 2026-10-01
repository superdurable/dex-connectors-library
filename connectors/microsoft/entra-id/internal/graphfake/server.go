// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package graphfake is a stateful, credential-safe stand-in for the Microsoft
// Graph user and group endpoints and the Microsoft identity platform token
// endpoint that the Entra ID connector calls. Tests route the connector to it
// with entraid.WithLocalProviderURL. Every error body carries MessageSentinel
// as its message so tests can prove the connector never repeats provider text.
package graphfake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// MessageSentinel is the message text of every fake error; it must never reach a Failure.
	MessageSentinel = "SENTINEL provider detail"
	// AccessToken is the bearer token the fake Graph accepts and the fake token endpoint issues.
	AccessToken = "graph-fake-access-token"
	// ClientID is the app registration client ID the fake token endpoint accepts.
	ClientID = "11111111-2222-4333-8444-555555555555"
	// ClientSecret is the client secret the fake token endpoint accepts; it is not shaped like a real one.
	ClientSecret = "graph-fake-client-secret"
	// TenantID is the directory ID the fake token endpoint serves.
	TenantID = "99999999-8888-4777-8666-555555555555"
	// RefreshToken is the refresh token the fake organizations endpoint accepts.
	RefreshToken = "graph-fake-refresh-token"
)

// Request is one recorded request. Body is the decoded JSON body, or the form values for token requests.
type Request struct {
	Method           string
	Path             string
	Query            url.Values
	ConsistencyLevel string
	Body             map[string]any
	Form             url.Values
}

// Answer is one status and JSON body; a nil Body writes no content.
type Answer struct {
	Status int
	Body   any
	// Delay, from AfterApply, holds the response after the state change while other requests proceed.
	Delay time.Duration
}

// Server is a stateful fake Microsoft Graph and token endpoint. Its exported hooks are set before use.
type Server struct {
	*httptest.Server
	mutex    sync.Mutex
	users    []map[string]any
	groups   map[string]map[string]bool
	nextID   int
	requests []Request
	// Intercept, when it returns true, answers a request without changing state.
	Intercept func(request Request, index int) (Answer, bool)
	// AfterApply sees every stateful answer and may replace or delay it, such as a lost response.
	AfterApply func(request Request, index int, answer Answer) Answer
	// RetryAfter, when set, is sent as a Retry-After header on every response.
	RetryAfter string
	// GrantedScope is the scope the fake organizations endpoint returns on refresh.
	GrantedScope string
}

// New starts a fake with no users or groups and closes it when the test ends.
func New(cleanup func(func())) *Server {
	server := &Server{groups: map[string]map[string]bool{},
		GrantedScope: "User.Read.All User.Create User.EnableDisableAccount.All User.RevokeSessions.All GroupMember.ReadWrite.All"}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serveHTTP))
	cleanup(server.Close)
	return server
}

// NewStandalone starts a fake that the caller closes.
func NewStandalone() *Server {
	server := &Server{groups: map[string]map[string]bool{},
		GrantedScope: "User.Read.All User.Create User.EnableDisableAccount.All User.RevokeSessions.All GroupMember.ReadWrite.All"}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serveHTTP))
	return server
}

func (server *Server) serveHTTP(response http.ResponseWriter, request *http.Request) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if server.RetryAfter != "" {
		response.Header().Set("Retry-After", server.RetryAfter)
	}
	response.Header().Set("request-id", "graph-fake-request")
	contents, err := io.ReadAll(request.Body)
	if err != nil {
		writeJSON(response, Answer{Status: http.StatusBadRequest, Body: GraphError("Request_BadRequest")})
		return
	}
	recorded := Request{Method: request.Method, Path: request.URL.Path, Query: request.URL.Query(), ConsistencyLevel: request.Header.Get("ConsistencyLevel")}
	if strings.HasSuffix(request.URL.Path, "/oauth2/v2.0/token") {
		// A malformed form keeps the fields it could parse, and the token checks reject the rest.
		recorded.Form, _ = url.ParseQuery(string(contents))
		server.requests = append(server.requests, recorded)
		writeJSON(response, server.answerToken(recorded))
		return
	}
	if len(contents) != 0 && json.Unmarshal(contents, &recorded.Body) != nil {
		writeJSON(response, Answer{Status: http.StatusBadRequest, Body: GraphError("Request_BadRequest")})
		return
	}
	server.requests = append(server.requests, recorded)
	index := len(server.requests) - 1
	if request.Header.Get("Authorization") != "Bearer "+AccessToken {
		writeJSON(response, Answer{Status: http.StatusUnauthorized, Body: GraphError("InvalidAuthenticationToken")})
		return
	}
	if server.Intercept != nil {
		if answer, handled := server.Intercept(recorded, index); handled {
			writeJSON(response, answer)
			return
		}
	}
	answer := server.answerGraph(recorded)
	if server.AfterApply != nil {
		answer = server.AfterApply(recorded, index, answer)
	}
	if answer.Delay > 0 {
		// Microsoft has applied the change; the response arrives late while other requests proceed.
		server.mutex.Unlock()
		time.Sleep(answer.Delay)
		server.mutex.Lock()
	}
	writeJSON(response, answer)
}

func (server *Server) answerToken(request Request) Answer {
	switch request.Form.Get("grant_type") {
	case "client_credentials":
		if request.Path != "/"+TenantID+"/oauth2/v2.0/token" || request.Form.Get("client_id") != ClientID ||
			request.Form.Get("client_secret") != ClientSecret || request.Form.Get("scope") != "https://graph.microsoft.com/.default" {
			return Answer{Status: http.StatusUnauthorized, Body: map[string]any{"error": "invalid_client", "error_description": MessageSentinel}}
		}
		return Answer{Status: http.StatusOK, Body: map[string]any{"token_type": "Bearer", "expires_in": 3599, "access_token": AccessToken}}
	case "refresh_token":
		if request.Path != "/organizations/oauth2/v2.0/token" || request.Form.Get("client_id") != ClientID ||
			request.Form.Get("client_secret") != ClientSecret || request.Form.Get("refresh_token") != RefreshToken {
			return Answer{Status: http.StatusBadRequest, Body: map[string]any{"error": "invalid_grant", "error_description": MessageSentinel}}
		}
		return Answer{Status: http.StatusOK, Body: map[string]any{
			"token_type": "Bearer", "expires_in": 3599, "access_token": AccessToken, "refresh_token": RefreshToken, "scope": server.GrantedScope,
		}}
	default:
		return Answer{Status: http.StatusBadRequest, Body: map[string]any{"error": "unsupported_grant_type", "error_description": MessageSentinel}}
	}
}

func (server *Server) answerGraph(request Request) Answer {
	segments := strings.Split(strings.TrimPrefix(request.Path, "/v1.0/"), "/")
	switch {
	case !strings.HasPrefix(request.Path, "/v1.0/"):
		return notFound()
	case request.Method == http.MethodPost && request.Path == "/v1.0/users":
		return server.createUser(request)
	case request.Method == http.MethodGet && request.Path == "/v1.0/users":
		return server.listUsers(request)
	case len(segments) >= 2 && segments[0] == "users" || strings.HasPrefix(segments[0], "users('"):
		return server.answerUser(request, segments)
	case len(segments) >= 2 && segments[0] == "groups":
		return server.answerGroup(request, segments)
	default:
		return notFound()
	}
}

func (server *Server) createUser(request Request) Answer {
	userPrincipalName, _ := request.Body["userPrincipalName"].(string)
	if server.findUser(userPrincipalName) != nil {
		return Answer{Status: http.StatusBadRequest, Body: ObjectConflictError()}
	}
	server.nextID++
	user := map[string]any{
		"id": ObjectID(server.nextID), "userPrincipalName": userPrincipalName, "displayName": request.Body["displayName"],
		"givenName": request.Body["givenName"], "surname": request.Body["surname"], "mailNickname": request.Body["mailNickname"],
		"jobTitle": request.Body["jobTitle"], "department": request.Body["department"], "usageLocation": request.Body["usageLocation"],
		"accountEnabled": request.Body["accountEnabled"], "userType": "Member", "onPremisesSyncEnabled": nil,
		"createdDateTime": "2026-10-01T09:00:00Z", "signInSessionsValidFromDateTime": "2026-10-01T09:00:00Z",
		"onPremisesExtensionAttributes": request.Body["onPremisesExtensionAttributes"], "passwordProfile": request.Body["passwordProfile"],
	}
	server.users = append(server.users, user)
	// The documented 201 body is the default property set, without accountEnabled or extension attributes.
	return Answer{Status: http.StatusCreated, Body: map[string]any{
		"@odata.context": "https://graph.microsoft.com/v1.0/$metadata#users/$entity", "id": user["id"],
		"userPrincipalName": user["userPrincipalName"], "displayName": user["displayName"], "givenName": user["givenName"],
		"surname": user["surname"], "jobTitle": user["jobTitle"], "mail": nil, "businessPhones": []any{},
	}}
}

func (server *Server) listUsers(request Request) Answer {
	top, err := strconv.Atoi(request.Query.Get("$top"))
	if err != nil || top < 1 {
		top = 100
	}
	// A first page has no skip token, which reads as offset zero.
	offset, _ := strconv.Atoi(request.Query.Get("$skiptoken"))
	if offset < 0 || offset > len(server.users) {
		offset = 0
	}
	end := min(offset+top, len(server.users))
	page := []any{}
	for _, user := range server.users[offset:end] {
		page = append(page, selectUserProperties(user, request.Query.Get("$select")))
	}
	body := map[string]any{"@odata.context": "https://graph.microsoft.com/v1.0/$metadata#users", "value": page}
	if end < len(server.users) {
		next := url.Values{}
		for key, values := range request.Query {
			next[key] = values
		}
		next.Set("$skiptoken", strconv.Itoa(end))
		body["@odata.nextLink"] = "https://graph.microsoft.com/v1.0/users?" + next.Encode()
	}
	return Answer{Status: http.StatusOK, Body: body}
}

func (server *Server) answerUser(request Request, segments []string) Answer {
	if strings.HasPrefix(segments[0], "users('") {
		quotedKey := strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(segments[0], "users('"), "')"), "''", "'")
		segments = append([]string{"users", quotedKey}, segments[1:]...)
	}
	userKey := segments[1]
	user := server.findUser(userKey)
	if user == nil {
		return notFound()
	}
	switch {
	case len(segments) == 2 && request.Method == http.MethodGet:
		return Answer{Status: http.StatusOK, Body: selectUserProperties(user, request.Query.Get("$select"))}
	case len(segments) == 2 && request.Method == http.MethodPatch:
		for key, value := range request.Body {
			user[key] = value
		}
		return Answer{Status: http.StatusNoContent}
	case len(segments) == 3 && segments[2] == "revokeSignInSessions" && request.Method == http.MethodPost:
		user["signInSessionsValidFromDateTime"] = "2026-10-01T10:00:00Z"
		return Answer{Status: http.StatusOK, Body: map[string]any{"@odata.context": "https://graph.microsoft.com/v1.0/$metadata#Edm.Boolean", "value": true}}
	case len(segments) == 2 && request.Method == http.MethodDelete:
		// The connector must never delete an account; record it so tests can fail loudly.
		user["deleted"] = true
		return Answer{Status: http.StatusNoContent}
	default:
		return notFound()
	}
}

func (server *Server) answerGroup(request Request, segments []string) Answer {
	members, exists := server.groups[strings.ToLower(segments[1])]
	if !exists {
		return notFound()
	}
	switch {
	case len(segments) == 2 && request.Method == http.MethodGet:
		return Answer{Status: http.StatusOK, Body: map[string]any{"id": strings.ToLower(segments[1])}}
	case len(segments) == 4 && segments[2] == "members" && segments[3] == "$ref" && request.Method == http.MethodPost:
		reference, _ := request.Body["@odata.id"].(string)
		userID, isDirectoryObject := strings.CutPrefix(reference, "https://graph.microsoft.com/v1.0/directoryObjects/")
		if !isDirectoryObject {
			return Answer{Status: http.StatusBadRequest, Body: GraphError("Request_BadRequest")}
		}
		user := server.findUser(userID)
		if user == nil {
			return notFound()
		}
		if members[user["id"].(string)] {
			return Answer{Status: http.StatusBadRequest, Body: GraphError("Request_BadRequest")}
		}
		members[user["id"].(string)] = true
		return Answer{Status: http.StatusNoContent}
	case len(segments) == 3 && segments[2] == "members" && request.Method == http.MethodGet:
		if request.ConsistencyLevel != "eventual" || request.Query.Get("$count") != "true" {
			return Answer{Status: http.StatusBadRequest, Body: GraphError("Request_UnsupportedQuery")}
		}
		filter := request.Query.Get("$filter")
		value := []any{}
		for userID := range members {
			if filter == "" || filter == "id eq '"+userID+"'" {
				value = append(value, map[string]any{"@odata.type": "#microsoft.graph.user", "id": userID})
			}
		}
		return Answer{Status: http.StatusOK, Body: map[string]any{"@odata.count": len(value), "value": value}}
	case len(segments) == 5 && segments[2] == "members" && segments[4] == "$ref" && request.Method == http.MethodDelete:
		if !members[strings.ToLower(segments[3])] {
			return notFound()
		}
		delete(members, strings.ToLower(segments[3]))
		return Answer{Status: http.StatusNoContent}
	default:
		return notFound()
	}
}

func (server *Server) findUser(userKey string) map[string]any {
	for _, user := range server.users {
		if strings.EqualFold(user["id"].(string), userKey) || strings.EqualFold(fmt.Sprint(user["userPrincipalName"]), userKey) {
			return user
		}
	}
	return nil
}

// SeedUser adds an existing account and returns its object ID.
func (server *Server) SeedUser(userPrincipalName string, isAccountEnabled bool, extensionAttributes map[string]any) string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.nextID++
	user := map[string]any{
		"id": ObjectID(server.nextID), "userPrincipalName": userPrincipalName, "displayName": "Existing Person",
		"givenName": "Existing", "surname": "Person", "mailNickname": strings.Split(userPrincipalName, "@")[0],
		"accountEnabled": isAccountEnabled, "userType": "Member", "onPremisesSyncEnabled": false,
		"createdDateTime": "2024-05-01T09:00:00Z", "signInSessionsValidFromDateTime": "2024-05-01T09:00:00Z",
		"onPremisesExtensionAttributes": extensionAttributes, "mobilePhone": "+1 650 555 0100",
	}
	server.users = append(server.users, user)
	return user["id"].(string)
}

// SeedGroup adds an empty group.
func (server *Server) SeedGroup(groupID string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.groups[strings.ToLower(groupID)] = map[string]bool{}
}

// SeedMember makes userID a direct member of groupID.
func (server *Server) SeedMember(groupID string, userID string) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.groups[strings.ToLower(groupID)][strings.ToLower(userID)] = true
}

// Requests returns a copy of the recorded requests.
func (server *Server) Requests() []Request {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]Request(nil), server.requests...)
}

// Count returns how many recorded requests match method and a path suffix.
func (server *Server) Count(method string, pathSuffix string) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	count := 0
	for _, request := range server.requests {
		if request.Method == method && strings.HasSuffix(request.Path, pathSuffix) {
			count++
		}
	}
	return count
}

// UserCount returns how many accounts exist.
func (server *Server) UserCount() int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return len(server.users)
}

// User returns a copy of one account's stored properties, or nil.
func (server *Server) User(userKey string) map[string]any {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	user := server.findUser(userKey)
	if user == nil {
		return nil
	}
	copied := map[string]any{}
	for key, value := range user {
		copied[key] = value
	}
	return copied
}

// IsMember reports whether userID is a direct member of groupID.
func (server *Server) IsMember(groupID string, userID string) bool {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.groups[strings.ToLower(groupID)][strings.ToLower(userID)]
}

// MemberCount returns how many direct members groupID has.
func (server *Server) MemberCount(groupID string) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return len(server.groups[strings.ToLower(groupID)])
}

// Passwords returns every generated password the fake received, in order.
func (server *Server) Passwords() []string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	passwords := []string{}
	for _, request := range server.requests {
		if request.Method != http.MethodPost || request.Path != "/v1.0/users" {
			continue
		}
		if profile, ok := request.Body["passwordProfile"].(map[string]any); ok {
			if password, ok := profile["password"].(string); ok {
				passwords = append(passwords, password)
			}
		}
	}
	return passwords
}

// ObjectID returns the deterministic GUID of the n-th object the fake creates.
func ObjectID(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

// GraphError is a Microsoft Graph error body with code and the sentinel message.
func GraphError(code string) map[string]any {
	return map[string]any{"error": map[string]any{
		"code": code, "message": MessageSentinel,
		"innerError": map[string]any{"date": "2026-10-01T09:00:00", "request-id": "graph-fake-request", "client-request-id": "graph-fake-request"},
	}}
}

// ObjectConflictError is the 400 Microsoft Entra returns for a taken unique property.
func ObjectConflictError() map[string]any {
	body := GraphError("Request_BadRequest")
	body["error"].(map[string]any)["details"] = []any{map[string]any{"code": "ObjectConflict", "message": MessageSentinel, "target": "userPrincipalName"}}
	return body
}

func notFound() Answer {
	return Answer{Status: http.StatusNotFound, Body: GraphError("Request_ResourceNotFound")}
}

// selectUserProperties returns the selected properties; accountEnabled and extension attributes
// are returned only when selected, as Microsoft documents.
func selectUserProperties(user map[string]any, selected string) map[string]any {
	properties := map[string]any{}
	if selected == "" {
		for _, name := range []string{"id", "userPrincipalName", "displayName", "givenName", "surname", "mail", "jobTitle"} {
			properties[name] = user[name]
		}
		return properties
	}
	for _, name := range strings.Split(selected, ",") {
		if value, exists := user[name]; exists {
			properties[name] = value
		} else {
			properties[name] = nil
		}
	}
	properties["id"] = user["id"]
	return properties
}

func writeJSON(response http.ResponseWriter, answer Answer) {
	if answer.Body == nil {
		response.WriteHeader(answer.Status)
		return
	}
	contents, err := json.Marshal(answer.Body)
	if err != nil {
		panic(err)
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(answer.Status)
	if _, err := response.Write(contents); err != nil {
		panic(err)
	}
}
