//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dealintake

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakePipedrive is a stateful, credential-checking Pipedrive API v2 and token endpoint; each fault affects the first matching request.
type fakePipedrive struct {
	*httptest.Server
	t               *testing.T
	mutex           sync.Mutex
	organizations   map[int]*fakeOrganization
	persons         map[int]*fakePerson
	deals           map[int]*fakeDeal
	nextID          int
	clock           time.Time
	counts          map[string]int
	requests        map[string][]fakeRecordedRequest
	delayedRequests sync.WaitGroup
	acceptedBearer  string

	delaysFirstPersonCreate        bool
	delaysFirstDealCreate          bool
	delaysFirstDealUpdate          bool
	losesFirstPersonCreateResponse bool
	losesFirstDealCreateResponse   bool
	rateLimitsFirstDealCreate      bool
	rejectsDealCreate              bool
	holdsFirstDealCreate           chan struct{}
}

type fakeOrganization struct {
	id   int
	name string
}

type fakePerson struct {
	id             int
	name           string
	email          string
	organizationID int
	ownerID        int
	updateTime     time.Time
}

type fakeDeal struct {
	id             int
	title          string
	personID       int
	organizationID int
	pipelineID     int
	stageID        int
	ownerID        int
	status         string
	customFields   map[string]json.RawMessage
	updateTime     time.Time
}

type fakeRecordedRequest struct {
	at    time.Time
	query url.Values
	body  string
}

func newFakePipedrive(t *testing.T) *fakePipedrive {
	t.Helper()
	provider := &fakePipedrive{
		t: t, organizations: map[int]*fakeOrganization{}, persons: map[int]*fakePerson{}, deals: map[int]*fakeDeal{},
		nextID: 100, clock: time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC),
		counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakePipedrive) serveHTTP(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	require.NoError(provider.t, err)
	if request.URL.Path == "/oauth/token" {
		provider.refreshToken(response, request, body)
		return
	}
	if !provider.isAuthenticated(request) {
		provider.writeJSON(response, http.StatusUnauthorized, `{"success":false,"error":"unauthorized access SENTINEL","errorCode":401}`)
		return
	}
	path, method := request.URL.Path, request.Method
	switch {
	case method == http.MethodGet && path == "/api/v2/organizations/search":
		provider.searchOrganizations(response, request, body)
	case method == http.MethodGet && path == "/api/v2/persons/search":
		provider.searchPersons(response, request, body)
	case method == http.MethodPost && path == "/api/v2/persons":
		provider.createPerson(response, request, body)
	case method == http.MethodPatch && strings.HasPrefix(path, "/api/v2/persons/"):
		provider.updatePerson(response, request, body, strings.TrimPrefix(path, "/api/v2/persons/"))
	case method == http.MethodGet && path == "/api/v2/deals":
		provider.listDeals(response, request, body)
	case method == http.MethodPost && path == "/api/v2/deals":
		provider.createDeal(response, request, body)
	case method == http.MethodPatch && strings.HasPrefix(path, "/api/v2/deals/"):
		provider.updateDeal(response, request, body, strings.TrimPrefix(path, "/api/v2/deals/"))
	case method == http.MethodGet && strings.HasPrefix(path, "/api/v2/deals/"):
		provider.readDeal(response, request, body, strings.TrimPrefix(path, "/api/v2/deals/"))
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"success":false,"error":"Unknown method SENTINEL","errorCode":404}`)
	}
}

func (provider *fakePipedrive) isAuthenticated(request *http.Request) bool {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if provider.acceptedBearer != "" {
		return request.Header.Get("Authorization") == "Bearer "+provider.acceptedBearer && request.Header.Get("X-Api-Token") == ""
	}
	return request.Header.Get("X-Api-Token") == integrationAPIToken && request.Header.Get("Authorization") == ""
}

func (provider *fakePipedrive) refreshToken(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("token", request, body)
	clientID, clientSecret, hasBasicAuthentication := request.BasicAuth()
	form, err := url.ParseQuery(string(body))
	require.NoError(provider.t, err)
	if !hasBasicAuthentication || clientID != integrationClientID || clientSecret != integrationClientSecret ||
		form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != integrationRefreshToken {
		provider.writeJSON(response, http.StatusBadRequest, `{"success":false,"message":"SENTINEL","error":"invalid_grant"}`)
		return
	}
	provider.mutex.Lock()
	provider.acceptedBearer = integrationRefreshedToken
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, map[string]any{
		"access_token": integrationRefreshedToken, "token_type": "Bearer", "expires_in": 3599, "refresh_token": integrationRefreshToken,
		"scope": "base,deals:full,contacts:full,users:read", "api_domain": provider.URL,
	})
}

func (provider *fakePipedrive) searchOrganizations(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("organizationSearch", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	items := []map[string]any{}
	term := strings.ToLower(request.URL.Query().Get("term"))
	for _, organization := range provider.sortedOrganizations() {
		// The fake also returns look-alike names such as "Acme Corp (EU)", which the Flow must not link.
		if name := strings.ToLower(organization.name); name == term || strings.HasPrefix(name, term+" (") {
			items = append(items, map[string]any{"result_score": 1, "item": map[string]any{"id": organization.id, "type": "organization", "name": organization.name}})
		}
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"items": items}, "additional_data": map[string]any{"next_cursor": nil}})
}

func (provider *fakePipedrive) searchPersons(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("personSearch", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	items := []map[string]any{}
	for _, person := range provider.sortedPersons() {
		if strings.EqualFold(person.email, request.URL.Query().Get("term")) {
			items = append(items, map[string]any{"result_score": 1, "item": map[string]any{
				"id": person.id, "type": "person", "name": person.name, "emails": []string{person.email},
				"owner": nullableLink(person.ownerID), "organization": nullableLink(person.organizationID),
			}})
		}
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"items": items}, "additional_data": map[string]any{"next_cursor": nil}})
}

type fakePersonWrite struct {
	Name   *string `json:"name"`
	Emails []struct {
		Value string `json:"value"`
	} `json:"emails"`
	OrganizationID *int `json:"org_id"`
	OwnerID        *int `json:"owner_id"`
}

func (provider *fakePipedrive) createPerson(response http.ResponseWriter, request *http.Request, body []byte) {
	attempt := provider.record("personCreate", request, body)
	var write fakePersonWrite
	require.NoError(provider.t, json.Unmarshal(body, &write))
	require.NotNil(provider.t, write.Name)
	require.Len(provider.t, write.Emails, 1)
	if attempt == 1 && provider.delaysFirstPersonCreate {
		provider.delay()
	}
	provider.mutex.Lock()
	person := &fakePerson{id: provider.allocateID(), name: *write.Name, email: write.Emails[0].Value, updateTime: provider.advanceClock()}
	applyPersonWrite(person, write)
	provider.persons[person.id] = person
	encoded := provider.personJSON(person)
	provider.mutex.Unlock()
	if attempt == 1 && provider.losesFirstPersonCreateResponse {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"success": true, "data": encoded})
}

func (provider *fakePipedrive) updatePerson(response http.ResponseWriter, request *http.Request, body []byte, personID string) {
	provider.record("personUpdate", request, body)
	var write fakePersonWrite
	require.NoError(provider.t, json.Unmarshal(body, &write))
	require.Empty(provider.t, write.Emails, "an update never rewrites the person's emails")
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	person := provider.persons[atoi(personID)]
	if person == nil {
		provider.writeJSON(response, http.StatusNotFound, `{"success":false,"error":"Person not found SENTINEL","errorCode":404}`)
		return
	}
	if write.Name != nil {
		person.name = *write.Name
	}
	applyPersonWrite(person, write)
	person.updateTime = provider.advanceClock()
	provider.writeValue(response, http.StatusOK, map[string]any{"success": true, "data": provider.personJSON(person)})
}

func applyPersonWrite(person *fakePerson, write fakePersonWrite) {
	if write.OrganizationID != nil {
		person.organizationID = *write.OrganizationID
	}
	if write.OwnerID != nil {
		person.ownerID = *write.OwnerID
	}
}

func (provider *fakePipedrive) listDeals(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("dealList", request, body)
	query := request.URL.Query()
	require.Equal(provider.t, "update_time", query.Get("sort_by"))
	require.Equal(provider.t, "desc", query.Get("sort_direction"))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	statuses := strings.Split(query.Get("status"), ",")
	deals := []map[string]any{}
	for _, deal := range provider.sortedDeals() {
		if strconv.Itoa(deal.personID) != query.Get("person_id") || strconv.Itoa(deal.pipelineID) != query.Get("pipeline_id") || !slices.Contains(statuses, deal.status) {
			continue
		}
		deals = append(deals, provider.dealJSON(deal))
	}
	slices.Reverse(deals)
	if limit := atoi(query.Get("limit")); limit > 0 && len(deals) > limit {
		deals = deals[:limit]
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"success": true, "data": deals, "additional_data": map[string]any{"next_cursor": nil}})
}

type fakeDealWrite struct {
	Title          *string                    `json:"title"`
	PersonID       *int                       `json:"person_id"`
	OrganizationID *int                       `json:"org_id"`
	PipelineID     *int                       `json:"pipeline_id"`
	StageID        *int                       `json:"stage_id"`
	OwnerID        *int                       `json:"owner_id"`
	CustomFields   map[string]json.RawMessage `json:"custom_fields"`
}

func (provider *fakePipedrive) createDeal(response http.ResponseWriter, request *http.Request, body []byte) {
	attempt := provider.record("dealCreate", request, body)
	var write fakeDealWrite
	require.NoError(provider.t, json.Unmarshal(body, &write))
	require.NotNil(provider.t, write.Title)
	switch {
	case attempt == 1 && provider.rateLimitsFirstDealCreate:
		response.Header().Set("X-Ratelimit-Reset", "1")
		response.Header().Set("X-Daily-Ratelimit-Token-Remaining", "29000")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"success":false,"error":"Rate limit exceeded SENTINEL","errorCode":429}`)
		return
	case provider.rejectsDealCreate:
		provider.writeJSON(response, http.StatusBadRequest, `{"success":false,"error":"Stage does not exist SENTINEL","errorCode":400}`)
		return
	case attempt == 1 && provider.delaysFirstDealCreate:
		provider.delay()
	}
	provider.mutex.Lock()
	deal := &fakeDeal{id: provider.allocateID(), title: *write.Title, status: "open", customFields: map[string]json.RawMessage{}, updateTime: provider.advanceClock()}
	applyDealWrite(deal, write)
	provider.deals[deal.id] = deal
	encoded := provider.dealJSON(deal)
	provider.mutex.Unlock()
	if attempt == 1 && provider.holdsFirstDealCreate != nil {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		<-provider.holdsFirstDealCreate
	}
	if attempt == 1 && provider.losesFirstDealCreateResponse {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"success": true, "data": encoded})
}

func (provider *fakePipedrive) updateDeal(response http.ResponseWriter, request *http.Request, body []byte, dealID string) {
	attempt := provider.record("dealUpdate", request, body)
	var write fakeDealWrite
	require.NoError(provider.t, json.Unmarshal(body, &write))
	if attempt == 1 && provider.delaysFirstDealUpdate {
		provider.delay()
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	deal := provider.deals[atoi(dealID)]
	if deal == nil {
		provider.writeJSON(response, http.StatusNotFound, `{"success":false,"error":"Deal not found SENTINEL","errorCode":404}`)
		return
	}
	applyDealWrite(deal, write)
	deal.updateTime = provider.advanceClock()
	provider.writeValue(response, http.StatusOK, map[string]any{"success": true, "data": provider.dealJSON(deal)})
}

func (provider *fakePipedrive) readDeal(response http.ResponseWriter, request *http.Request, body []byte, dealID string) {
	provider.record("dealRead", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	deal := provider.deals[atoi(dealID)]
	if deal == nil {
		provider.writeJSON(response, http.StatusNotFound, `{"success":false,"error":"Deal not found SENTINEL","errorCode":404}`)
		return
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"success": true, "data": provider.dealJSON(deal)})
}

func applyDealWrite(deal *fakeDeal, write fakeDealWrite) {
	for destination, value := range map[*int]*int{
		&deal.personID: write.PersonID, &deal.organizationID: write.OrganizationID, &deal.pipelineID: write.PipelineID,
		&deal.stageID: write.StageID, &deal.ownerID: write.OwnerID,
	} {
		if value != nil {
			*destination = *value
		}
	}
	if write.Title != nil {
		deal.title = *write.Title
	}
	for key, value := range write.CustomFields {
		deal.customFields[key] = value
	}
}

// record counts and stores one request and returns its 1-based number among requests with the same name.
func (provider *fakePipedrive) record(name string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	provider.requests[name] = append(provider.requests[name], fakeRecordedRequest{at: time.Now(), query: request.URL.Query(), body: string(body)})
	return provider.counts[name]
}

// delay outlasts Dex's roughly seven-second async local phase; the caller holds no lock.
func (provider *fakePipedrive) delay() {
	provider.delayedRequests.Add(1)
	defer provider.delayedRequests.Done()
	time.Sleep(slowResponseDelay)
}

// dropConnection closes the connection after Pipedrive applied the request, as a lost response would.
func (provider *fakePipedrive) dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(provider.t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(provider.t, err)
	require.NoError(provider.t, connection.Close())
}

// routingClient sends https://oauth.pipedrive.com requests to the fake and refuses every other remote host.
func (provider *fakePipedrive) routingClient(t *testing.T, timeout time.Duration) *http.Client {
	target, err := url.Parse(provider.URL)
	require.NoError(t, err)
	return &http.Client{Timeout: timeout, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		routed := request.Clone(request.Context())
		switch request.URL.Host {
		case "oauth.pipedrive.com":
			routed.URL.Scheme, routed.URL.Host = target.Scheme, target.Host
		case target.Host:
		default:
			return nil, errors.New("request to an unexpected host")
		}
		return http.DefaultTransport.RoundTrip(routed)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

// allocateID returns the next record ID; the caller holds the mutex.
func (provider *fakePipedrive) allocateID() int {
	provider.nextID++
	return provider.nextID
}

// advanceClock returns a later update time for every write; the caller holds the mutex.
func (provider *fakePipedrive) advanceClock() time.Time {
	provider.clock = provider.clock.Add(time.Minute)
	return provider.clock
}

func (provider *fakePipedrive) seedOrganization(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	organization := &fakeOrganization{id: provider.allocateID(), name: name}
	provider.organizations[organization.id] = organization
	return organization.id
}

func (provider *fakePipedrive) seedPerson(name string, email string, organizationID int) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	person := &fakePerson{id: provider.allocateID(), name: name, email: email, organizationID: organizationID, updateTime: provider.advanceClock()}
	provider.persons[person.id] = person
	return person.id
}

func (provider *fakePipedrive) seedDeal(title string, personID int, pipelineID int, stageID int, status string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	deal := &fakeDeal{
		id: provider.allocateID(), title: title, personID: personID, pipelineID: pipelineID, stageID: stageID, status: status,
		customFields: map[string]json.RawMessage{}, updateTime: provider.advanceClock(),
	}
	provider.deals[deal.id] = deal
	return deal.id
}

func (provider *fakePipedrive) personJSON(person *fakePerson) map[string]any {
	return map[string]any{
		"id": person.id, "name": person.name, "owner_id": nullableID(person.ownerID), "org_id": nullableID(person.organizationID),
		"emails":   []map[string]any{{"value": person.email, "primary": true, "label": "work"}},
		"add_time": provider.clock.Format(time.RFC3339), "update_time": person.updateTime.Format(time.RFC3339), "custom_fields": map[string]any{},
	}
}

func (provider *fakePipedrive) dealJSON(deal *fakeDeal) map[string]any {
	return map[string]any{
		"id": deal.id, "title": deal.title, "owner_id": nullableID(deal.ownerID), "person_id": nullableID(deal.personID),
		"org_id": nullableID(deal.organizationID), "pipeline_id": deal.pipelineID, "stage_id": deal.stageID, "status": deal.status,
		"value": 0, "currency": "USD", "add_time": provider.clock.Format(time.RFC3339), "update_time": deal.updateTime.Format(time.RFC3339),
		"custom_fields": deal.customFields,
	}
}

// sortedOrganizations, sortedPersons, and sortedDeals return records by ID; the caller holds the mutex.
func (provider *fakePipedrive) sortedOrganizations() []*fakeOrganization {
	return sortedByID(provider.organizations)
}

func (provider *fakePipedrive) sortedPersons() []*fakePerson {
	return sortedByID(provider.persons)
}

func (provider *fakePipedrive) sortedDeals() []*fakeDeal {
	return sortedByID(provider.deals)
}

func sortedByID[T any](records map[int]*T) []*T {
	ids := make([]int, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	sorted := make([]*T, 0, len(ids))
	for _, id := range ids {
		sorted = append(sorted, records[id])
	}
	return sorted
}

func (provider *fakePipedrive) writeValue(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	provider.writeJSON(response, status, string(encoded))
}

func (provider *fakePipedrive) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Correlation-Id", "d853687f-f72e-4ed9-bbad-be98638737c8")
	response.Header().Set("X-Ratelimit-Limit", "80")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake Pipedrive response write failed: %v", err)
	}
}

func (provider *fakePipedrive) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * slowResponseDelay):
		t.Fatal("a delayed fake Pipedrive request did not finish")
	}
}

func (provider *fakePipedrive) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakePipedrive) requestsNamed(name string) []fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]fakeRecordedRequest(nil), provider.requests[name]...)
}

func (provider *fakePipedrive) personCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.persons)
}

func (provider *fakePipedrive) dealCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.deals)
}

func (provider *fakePipedrive) person(personID int) fakePerson {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return *provider.persons[personID]
}

func (provider *fakePipedrive) deal(dealID int) fakeDeal {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return *provider.deals[dealID]
}

func nullableID(id int) any {
	if id == 0 {
		return nil
	}
	return id
}

func nullableLink(id int) any {
	if id == 0 {
		return nil
	}
	return map[string]any{"id": id}
}

func atoi(value string) int {
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return number
}
