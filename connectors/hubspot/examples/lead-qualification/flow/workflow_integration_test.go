//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadqualification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAccessToken = "pat-na1-integration-token"
	integrationOwnerID     = "77"
	integrationPipelineID  = "default"
	integrationStageID     = "qualifiedtobuy"
	openingStageID         = "appointmentscheduled"
	sentinelMessage        = "SENTINEL provider message text"

	existingLeadEmail  = "ada@example.com"
	rejectedLeadEmail  = "rejected@example.com"
	limitedLeadEmail   = "rate-limited@example.com"
	slowLeadEmail      = "slow@example.com"
	vanishingLeadEmail = "vanishing@example.com"

	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so Dex dispatches again.
	slowResponseDelay = 9 * time.Second
)

var integrationSettings = Settings{
	LeadOwner:          LeadOwnerConfiguration{OwnerID: integrationOwnerID},
	QualifiedDealStage: QualifiedDealStageConfiguration{PipelineID: integrationPipelineID, StageID: integrationStageID},
}

func TestExistingLeadAdvancesItsOpenDealWithRealDex(t *testing.T) {
	provider, harness := newLeadQualificationHarness(t)
	ctx := integrationContext(t, time.Minute)
	flowID := uniqueFlowID("existing")
	requestID := "start-" + flowID
	options := dex.StartFlowOptions{RequestID: &requestID, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}}
	input := Input{Email: existingLeadEmail, FirstName: "Ada", LastName: "Lovelace", Company: "Analytical Engines"}

	runID, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, options)
	require.NoError(t, err)
	repeatedRunID, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, options)
	require.NoError(t, err, "a retry of the same logical start attaches to the existing run")
	require.Equal(t, runID, repeatedRunID)

	qualification := waitForCompletedQualification(t, ctx, harness, flowID)
	require.Equal(t, PhaseDealAdvanced, qualification.Phase)
	require.Equal(t, "501", qualification.ContactID)
	require.False(t, qualification.IsContactCreated)
	require.Equal(t, "9001", qualification.DealID)
	require.Equal(t, "Analytical Engines renewal", qualification.DealName)
	require.Equal(t, openingStageID, qualification.PreviousDealStageID)
	require.Equal(t, integrationStageID, qualification.DealStageID)

	contact := provider.contactByEmail(existingLeadEmail)
	require.Equal(t, "Ada", contact.properties["firstname"])
	require.Equal(t, integrationOwnerID, contact.properties["hubspot_owner_id"])
	require.Equal(t, integrationStageID, provider.deal("9001").properties["dealstage"])
	require.Equal(t, openingStageID, provider.deal("9002").properties["dealstage"], "the closed deal is a decoy")
	require.Equal(t, openingStageID, provider.deal("9003").properties["dealstage"], "the other pipeline's deal is a decoy")
	require.Equal(t, 1, provider.count("upsert "+existingLeadEmail), "the duplicate start ran the Flow once")
	require.Equal(t, 1, provider.count("patch 9001"))
	require.Equal(t, 1, provider.count("get 9001"))
}

func TestNewLeadWithoutAnOpenDealCompletesWithRealDex(t *testing.T) {
	provider, harness := newLeadQualificationHarness(t)
	ctx := integrationContext(t, time.Minute)
	flowID := startQualification(t, ctx, harness, "new", Input{Email: "grace@example.com", FirstName: "Grace"})

	qualification := waitForCompletedQualification(t, ctx, harness, flowID)
	require.Equal(t, PhaseNoOpenDeal, qualification.Phase)
	require.True(t, qualification.IsContactCreated)
	require.Empty(t, qualification.DealID)
	contact := provider.contactByEmail("grace@example.com")
	require.Equal(t, qualification.ContactID, contact.id)
	require.Equal(t, integrationOwnerID, contact.properties["hubspot_owner_id"])
	require.Equal(t, 1, provider.contactCount("grace@example.com"))
	require.Equal(t, 1, provider.count("search "+qualification.ContactID))
	require.Zero(t, provider.count("patch 9001"))
}

func TestRateLimitedUpsertIsRetriedAfterRetryAfterWithRealDex(t *testing.T) {
	provider, harness := newLeadQualificationHarness(t)
	ctx := integrationContext(t, time.Minute)
	flowID := startQualification(t, ctx, harness, "rate-limited", Input{Email: limitedLeadEmail})

	qualification := waitForCompletedQualification(t, ctx, harness, flowID)
	require.Equal(t, PhaseNoOpenDeal, qualification.Phase)
	attempts := provider.times("upsert " + limitedLeadEmail)
	require.Len(t, attempts, 2, "the 429 created nothing, so Dex retried the upsert once")
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), 900*time.Millisecond, "Dex honored HubSpot's Retry-After delay")
	require.Equal(t, 1, provider.contactCount(limitedLeadEmail))
}

// TestSlowWritesDispatchedTwiceConvergeOnOneRecordWithRealDex proves async durability survives Dex's repeated dispatch of slow writes.
func TestSlowWritesDispatchedTwiceConvergeOnOneRecordWithRealDex(t *testing.T) {
	provider, harness := newLeadQualificationHarness(t)
	ctx := integrationContext(t, 2*time.Minute)
	flowID := startQualification(t, ctx, harness, "slow", Input{Email: slowLeadEmail, FirstName: "Katherine"})

	qualification := waitForCompletedQualification(t, ctx, harness, flowID)
	t.Logf("Flow %s: %d upsert and %d update dispatches", flowID, provider.count("upsert "+slowLeadEmail), provider.count("patch 9101"))
	require.Equal(t, PhaseDealAdvanced, qualification.Phase)
	require.GreaterOrEqual(t, provider.count("upsert "+slowLeadEmail), 2, "Dex's async fallback dispatched the slow upsert again")
	require.Equal(t, 1, provider.contactCount(slowLeadEmail), "the repeated upsert updated the contact the first dispatch created")
	require.Equal(t, "Katherine", provider.contactByEmail(slowLeadEmail).properties["firstname"])
	require.GreaterOrEqual(t, provider.count("patch 9101"), 2, "Dex's async fallback dispatched the slow update again")
	require.Equal(t, integrationStageID, provider.deal("9101").properties["dealstage"])
	require.Equal(t, integrationStageID, qualification.DealStageID)
}

func TestRejectedUpsertCompletesWithASecretSafeFailureWithRealDex(t *testing.T) {
	provider, harness := newLeadQualificationHarness(t)
	ctx := integrationContext(t, time.Minute)
	flowID := startQualification(t, ctx, harness, "rejected", Input{Email: rejectedLeadEmail})

	qualification := waitForCompletedQualification(t, ctx, harness, flowID)
	require.Equal(t, PhaseRejected, qualification.Phase)
	require.NotNil(t, qualification.Rejection)
	require.Equal(t, sdkgo.FailureValidation, qualification.Rejection.Kind)
	require.Contains(t, qualification.Rejection.Message, "INVALID_EMAIL")
	encoded, err := json.Marshal(qualification)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), sentinelMessage)
	require.NotContains(t, string(encoded), integrationAccessToken)
	require.Equal(t, 1, provider.count("upsert "+rejectedLeadEmail), "a validation rejection is not retried")
	require.Zero(t, provider.contactCount(rejectedLeadEmail))
}

func TestDealDeletedBeforeTheUpdateFailsTheFlowThroughTheUnwiredNotFoundBranchWithRealDex(t *testing.T) {
	provider, harness := newLeadQualificationHarness(t)
	ctx := integrationContext(t, time.Minute)
	flowID := startQualification(t, ctx, harness, "vanishing", Input{Email: vanishingLeadEmail})

	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.NotContains(t, result.ErrorMessage, integrationAccessToken)
	require.Equal(t, 1, provider.count("patch 9201"), "an optional branch fails the Flow without an Execute retry")
	require.Zero(t, provider.count("get 9201"))
}

func TestExpiredOAuthConnectionRefreshesBeforeTheUpsertWithRealDex(t *testing.T) {
	provider := newFakeHubSpot(t, "refreshed-oauth-access-token")
	tokenRequests := 0
	var tokenMutex sync.Mutex
	httpClient := &http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, request *http.Request) {
		tokenMutex.Lock()
		tokenRequests++
		tokenMutex.Unlock()
		if request.ParseForm() != nil || request.PostForm.Get("refresh_token") != "stored-refresh-token" {
			writeProviderJSON(t, response, http.StatusBadRequest, `{"error":"invalid_grant","status":"BAD_REFRESH_TOKEN"}`)
			return
		}
		writeProviderJSON(t, response, http.StatusOK, `{"token_type":"bearer","access_token":"refreshed-oauth-access-token",
			"refresh_token":"rotated-refresh-token","expires_in":1800,"hub_id":1234567}`)
	}}}
	connectionsPath := writeOAuthConnections(t, provider.URL)
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)
	connection, err := hubspot.NewLocalConnection(store, ConnectionName, hubspot.WithHTTPClient(httpClient))
	require.NoError(t, err)
	harness := newHarnessForConnection(t, connection)
	ctx := integrationContext(t, time.Minute)
	flowID := startQualification(t, ctx, harness, "oauth", Input{Email: existingLeadEmail})

	qualification := waitForCompletedQualification(t, ctx, harness, flowID)
	require.Equal(t, PhaseDealAdvanced, qualification.Phase)
	tokenMutex.Lock()
	require.Equal(t, 1, tokenRequests, "one refresh serves every later call until the new token nears expiry")
	tokenMutex.Unlock()
	contents, err := os.ReadFile(connectionsPath)
	require.NoError(t, err)
	require.Contains(t, string(contents), "refreshed-oauth-access-token")
	require.Contains(t, string(contents), "rotated-refresh-token")
	require.NotContains(t, string(contents), "expired-oauth-access-token")
	require.Contains(t, string(contents), `"authMethodId": "hubspot-oauth"`, "the refresh rewrite keeps Dex Web's record member")
}

func startQualification(t *testing.T, ctx context.Context, harness *leadQualificationHarness, scenario string, input Input) string {
	t.Helper()
	flowID := uniqueFlowID(scenario)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func waitForCompletedQualification(t *testing.T, ctx context.Context, harness *leadQualificationHarness, flowID string) LeadQualification {
	t.Helper()
	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var qualification LeadQualification
	require.NoError(t, result.DecodeSingleOutput(&qualification))
	return qualification
}

func waitForTerminalFlow(t *testing.T, ctx context.Context, client *dex.Client, flowID string) dex.FlowResult {
	t.Helper()
	for {
		result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not reach a terminal status", flowID)
		return result
	}
}

func uniqueFlowID(scenario string) string {
	return "hubspot-lead-qualification-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

func integrationContext(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}

// fakeHubSpot is a stateful fake that, like HubSpot, applies a repeated upsert of one email to one contact.
type fakeHubSpot struct {
	*httptest.Server
	t           *testing.T
	accessToken string
	mutex       sync.Mutex
	contacts    map[string]*fakeRecord
	deals       map[string]*fakeRecord
	nextID      int
	calls       map[string][]time.Time
}

type fakeRecord struct {
	id                string
	properties        map[string]string
	contactID         string
	isDeletedOnUpdate bool
}

func newFakeHubSpot(t *testing.T, accessToken string) *fakeHubSpot {
	t.Helper()
	provider := &fakeHubSpot{
		t: t, accessToken: accessToken, contacts: map[string]*fakeRecord{}, deals: map[string]*fakeRecord{},
		nextID: 700, calls: map[string][]time.Time{},
	}
	provider.seedContactWithDeals(existingLeadEmail, "501", "9001", "Analytical Engines renewal")
	provider.deals["9002"] = &fakeRecord{id: "9002", contactID: "501", properties: dealProperties("Closed deal", "true", integrationPipelineID)}
	provider.deals["9003"] = &fakeRecord{id: "9003", contactID: "501", properties: dealProperties("Other pipeline", "false", "partner-pipeline")}
	provider.seedContactWithDeals(slowLeadEmail, "511", "9101", "Slow renewal")
	provider.seedContactWithDeals(vanishingLeadEmail, "521", "9201", "Deleted deal")
	provider.deals["9201"].isDeletedOnUpdate = true
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeHubSpot) seedContactWithDeals(email string, contactID string, dealID string, dealName string) {
	provider.contacts[email] = &fakeRecord{id: contactID, properties: map[string]string{"email": email}}
	provider.deals[dealID] = &fakeRecord{id: dealID, contactID: contactID, properties: dealProperties(dealName, "false", integrationPipelineID)}
}

func dealProperties(name string, isClosed string, pipeline string) map[string]string {
	return map[string]string{"dealname": name, "dealstage": openingStageID, "pipeline": pipeline, "hs_is_closed": isClosed}
}

func (provider *fakeHubSpot) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+provider.accessToken {
		writeProviderJSON(provider.t, response, http.StatusUnauthorized, `{"status":"error","category":"INVALID_AUTHENTICATION","message":"`+sentinelMessage+`"}`)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		writeProviderJSON(provider.t, response, http.StatusBadRequest, `{"category":"VALIDATION_ERROR"}`)
		return
	}
	const objectsPrefix = "/crm/objects/2026-09/"
	path := strings.TrimPrefix(request.URL.Path, objectsPrefix)
	switch {
	case request.Method == http.MethodPost && path == "contacts/batch/upsert":
		provider.upsertContact(response, body)
	case request.Method == http.MethodPost && path == "deals/search":
		provider.searchDeals(response, body)
	case request.Method == http.MethodPatch && strings.HasPrefix(path, "deals/"):
		provider.updateDeal(response, strings.TrimPrefix(path, "deals/"), body)
	case request.Method == http.MethodGet && strings.HasPrefix(path, "deals/"):
		provider.readDeal(response, strings.TrimPrefix(path, "deals/"))
	default:
		writeProviderJSON(provider.t, response, http.StatusNotFound, `{"category":"OBJECT_NOT_FOUND"}`)
	}
}

func (provider *fakeHubSpot) upsertContact(response http.ResponseWriter, body []byte) {
	var request struct {
		Inputs []struct {
			IDProperty string            `json:"idProperty"`
			ID         string            `json:"id"`
			Properties map[string]string `json:"properties"`
		} `json:"inputs"`
	}
	if json.Unmarshal(body, &request) != nil || len(request.Inputs) != 1 || request.Inputs[0].IDProperty != "email" {
		writeProviderJSON(provider.t, response, http.StatusBadRequest, `{"category":"VALIDATION_ERROR"}`)
		return
	}
	input := request.Inputs[0]
	attempt := provider.record("upsert " + input.ID)
	switch {
	case input.ID == rejectedLeadEmail:
		writeProviderJSON(provider.t, response, http.StatusBadRequest, `{"status":"error","message":"`+sentinelMessage+`",
			"category":"VALIDATION_ERROR","errors":[{"code":"INVALID_EMAIL","message":"`+sentinelMessage+`"}]}`)
		return
	case input.ID == limitedLeadEmail && attempt == 1:
		response.Header().Set("Retry-After", "1")
		writeProviderJSON(provider.t, response, http.StatusTooManyRequests, `{"status":"error","errorType":"RATE_LIMIT","policyName":"TEN_SECONDLY_ROLLING"}`)
		return
	}
	provider.mutex.Lock()
	contact, exists := provider.contacts[input.ID]
	if !exists {
		provider.nextID++
		contact = &fakeRecord{id: strconv.Itoa(provider.nextID), properties: map[string]string{"email": input.ID}}
		provider.contacts[input.ID] = contact
	}
	for name, value := range input.Properties {
		contact.properties[name] = value
	}
	encoded := provider.encodeRecord(contact, map[string]any{"new": !exists})
	provider.mutex.Unlock()
	if input.ID == slowLeadEmail {
		time.Sleep(slowResponseDelay)
	}
	writeProviderJSON(provider.t, response, http.StatusOK, `{"status":"COMPLETE","results":[`+encoded+`]}`)
}

func (provider *fakeHubSpot) searchDeals(response http.ResponseWriter, body []byte) {
	var request struct {
		FilterGroups []struct {
			Filters []struct {
				PropertyName string `json:"propertyName"`
				Operator     string `json:"operator"`
				Value        string `json:"value"`
			} `json:"filters"`
		} `json:"filterGroups"`
		Limit int `json:"limit"`
	}
	if json.Unmarshal(body, &request) != nil || len(request.FilterGroups) != 1 || request.Limit < 1 {
		writeProviderJSON(provider.t, response, http.StatusBadRequest, `{"category":"VALIDATION_ERROR"}`)
		return
	}
	filters := request.FilterGroups[0].Filters
	for _, filter := range filters {
		if filter.PropertyName == "associations.contact" {
			provider.record("search " + filter.Value)
		}
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	dealIDs := make([]string, 0, len(provider.deals))
	for dealID := range provider.deals {
		dealIDs = append(dealIDs, dealID)
	}
	slices.Sort(dealIDs)
	var matches []string
	for _, dealID := range dealIDs {
		deal := provider.deals[dealID]
		isMatch := true
		for _, filter := range filters {
			value := deal.properties[filter.PropertyName]
			if filter.PropertyName == "associations.contact" {
				value = deal.contactID
			}
			isMatch = isMatch && filter.Operator == "EQ" && value == filter.Value
		}
		if isMatch {
			matches = append(matches, provider.encodeRecord(deal, nil))
		}
	}
	if len(matches) > request.Limit {
		matches = matches[:request.Limit]
	}
	writeProviderJSON(provider.t, response, http.StatusOK, `{"total":`+strconv.Itoa(len(matches))+`,"results":[`+strings.Join(matches, ",")+`]}`)
}

func (provider *fakeHubSpot) updateDeal(response http.ResponseWriter, dealID string, body []byte) {
	provider.record("patch " + dealID)
	var request struct {
		Properties map[string]string `json:"properties"`
	}
	if json.Unmarshal(body, &request) != nil || len(request.Properties) == 0 {
		writeProviderJSON(provider.t, response, http.StatusBadRequest, `{"category":"VALIDATION_ERROR"}`)
		return
	}
	provider.mutex.Lock()
	deal, exists := provider.deals[dealID]
	if exists && deal.isDeletedOnUpdate {
		delete(provider.deals, dealID)
		exists = false
	}
	if !exists {
		provider.mutex.Unlock()
		writeProviderJSON(provider.t, response, http.StatusNotFound, `{"status":"error","category":"OBJECT_NOT_FOUND","message":"`+sentinelMessage+`"}`)
		return
	}
	for name, value := range request.Properties {
		deal.properties[name] = value
	}
	encoded := provider.encodeRecord(deal, nil)
	provider.mutex.Unlock()
	if dealID == "9101" {
		time.Sleep(slowResponseDelay)
	}
	writeProviderJSON(provider.t, response, http.StatusOK, encoded)
}

func (provider *fakeHubSpot) readDeal(response http.ResponseWriter, dealID string) {
	provider.record("get " + dealID)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	deal, exists := provider.deals[dealID]
	if !exists {
		writeProviderJSON(provider.t, response, http.StatusNotFound, `{"category":"OBJECT_NOT_FOUND"}`)
		return
	}
	writeProviderJSON(provider.t, response, http.StatusOK, provider.encodeRecord(deal, nil))
}

// encodeRecord renders a record as HubSpot does, plus any endpoint-specific fields; callers hold the mutex.
func (provider *fakeHubSpot) encodeRecord(record *fakeRecord, extraFields map[string]any) string {
	fields := map[string]any{
		"id": record.id, "properties": record.properties, "archived": false,
		"createdAt": "2026-09-01T10:00:00.000Z", "updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}
	for name, value := range extraFields {
		fields[name] = value
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		provider.t.Errorf("encode fake HubSpot record: %v", err)
	}
	return string(encoded)
}

func (provider *fakeHubSpot) record(call string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.calls[call] = append(provider.calls[call], time.Now())
	return len(provider.calls[call])
}

func (provider *fakeHubSpot) count(call string) int {
	return len(provider.times(call))
}

func (provider *fakeHubSpot) times(call string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]time.Time(nil), provider.calls[call]...)
}

func (provider *fakeHubSpot) contactCount(email string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if _, exists := provider.contacts[email]; exists {
		return 1
	}
	return 0
}

func (provider *fakeHubSpot) contactByEmail(email string) fakeRecord {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	contact, exists := provider.contacts[email]
	require.True(provider.t, exists, email)
	return fakeRecord{id: contact.id, properties: copyProperties(contact.properties)}
}

func (provider *fakeHubSpot) deal(dealID string) fakeRecord {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	deal, exists := provider.deals[dealID]
	require.True(provider.t, exists, dealID)
	return fakeRecord{id: deal.id, properties: copyProperties(deal.properties)}
}

func copyProperties(properties map[string]string) map[string]string {
	copied := make(map[string]string, len(properties))
	for name, value := range properties {
		copied[name] = value
	}
	return copied
}

func writeProviderJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		t.Logf("fake HubSpot response write failed: %v", err)
	}
}

// tokenEndpointTransport serves HubSpot's fixed OAuth token URL from a handler and sends every other request normally.
type tokenEndpointTransport struct {
	tokenHandler http.HandlerFunc
}

func (transport tokenEndpointTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme == "https" && request.URL.Host == "api.hubapi.com" && request.URL.Path == "/oauth/2026-09/token" {
		recorder := httptest.NewRecorder()
		transport.tokenHandler(recorder, request)
		return recorder.Result(), nil
	}
	return http.DefaultTransport.RoundTrip(request)
}

func writeOAuthConnections(t *testing.T, endpoint string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "connections.json")
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []map[string]any{{
			"connectorId": hubspot.ConnectorID, "authMethodId": hubspot.OAuthAuthMethodID,
			"modulePath":    "github.com/superdurable/dex-connectors-library/connectors/hubspot",
			"moduleVersion": "v0.1.0", "provider": "hubspot", "connectionName": ConnectionName,
			"configuration": map[string]any{"endpoint": endpoint},
			"credentials": map[string]any{
				"auth_method": hubspot.OAuthAuthMethodID, "oauth_client_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
				"oauth_client_secret": "oauth-client-secret", "access_token": "expired-oauth-access-token",
				"refresh_token": "stored-refresh-token",
			},
			"credentialExpiresAt": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	return path
}

type leadQualificationHarness struct {
	flow         *Flow
	cache        *blobcache.Cache
	worker       *dex.Worker
	workerResult chan error
	client       *dex.Client
}

func newLeadQualificationHarness(t *testing.T) (*fakeHubSpot, *leadQualificationHarness) {
	t.Helper()
	provider := newFakeHubSpot(t, integrationAccessToken)
	reference := sdkgo.ConnectionRef{Provider: "hubspot", Name: ConnectionName}
	client, err := hubspot.New(hubspot.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[hubspot.Credentials]{
		reference: {AuthMethodID: hubspot.PrivateAppTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(integrationAccessToken)},
	}, hubspot.WithHTTPClient(&http.Client{Timeout: 20 * time.Second}))
	require.NoError(t, err)
	connection, err := hubspot.NewConnection(client, reference)
	require.NoError(t, err)
	return provider, newHarnessForConnection(t, connection)
}

func newHarnessForConnection(t *testing.T, connection hubspot.Connection) *leadQualificationHarness {
	t.Helper()
	flow, err := NewFlow(connection, &integrationSettings)
	require.NoError(t, err)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &leadQualificationHarness{flow: flow, cache: cache}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- harness.worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

func availableIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
