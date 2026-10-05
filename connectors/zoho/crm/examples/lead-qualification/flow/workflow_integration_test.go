//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadqualification

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm/internal/fakecrm"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAccessToken    = "1000.zohoCRMIntegrationAccess0123456789"
	integrationRefreshedToken = "1000.zohoCRMIntegrationRefreshed0123456789"
	integrationRefreshToken   = "1000.zohoCRMIntegrationRefresh0123456789"
	integrationOwnerID        = "4150868000000225099"

	defaultRequestTimeout = 5 * time.Second
	slowRequestTimeout    = 20 * time.Second
)

var seededAt = time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC)

func integrationInput() Input {
	return Input{
		ContactEmail: "jane@acme.example.com", ContactFirstName: "Jane", ContactLastName: "Smith",
		AccountName: "Acme Corp", Stage: "Proposal/Price Quote", OwnerID: integrationOwnerID,
	}
}

// seedJaneWithDeals returns the account, contact, newest open deal, and three decoy deals.
func seedJaneWithDeals(provider *fakecrm.Server) (string, string, string, []string) {
	accountID := provider.SeedRecord("Accounts", map[string]any{"Account_Name": "Acme Corp"}, seededAt)
	contactID := provider.SeedRecord("Contacts", map[string]any{
		"Email": "JANE@acme.example.com", "Last_Name": "Smith", "Account_Name": map[string]any{"id": accountID},
	}, seededAt)
	otherContactID := provider.SeedRecord("Contacts", map[string]any{"Email": "jane.smith@acme.example.com", "Last_Name": "Smith-Okafor"}, seededAt)
	olderOpen := provider.SeedRecord("Deals", map[string]any{"Deal_Name": "Acme renewal", "Stage": "Qualification",
		"Contact_Name": map[string]any{"id": contactID}}, seededAt.Add(time.Hour))
	newestOpen := provider.SeedRecord("Deals", map[string]any{"Deal_Name": "Acme expansion", "Stage": "Negotiation/Review",
		"Contact_Name": map[string]any{"id": contactID}}, seededAt.Add(2*time.Hour))
	closedNewer := provider.SeedRecord("Deals", map[string]any{"Deal_Name": "Acme expansion", "Stage": "Closed Won",
		"Contact_Name": map[string]any{"id": contactID}}, seededAt.Add(3*time.Hour))
	otherContactDeal := provider.SeedRecord("Deals", map[string]any{"Deal_Name": "Acme expansion", "Stage": "Qualification",
		"Contact_Name": map[string]any{"id": otherContactID}}, seededAt.Add(4*time.Hour))
	return accountID, contactID, newestOpen, []string{olderOpen, closedNewer, otherContactDeal}
}

func TestNewLeadCreatesTheAccountAndLinkedContactWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	harness := newQualificationHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runQualification(t, "new-lead", integrationInput())
	require.Equal(t, PhaseNoOpenDeal, outcome.Phase)
	require.True(t, outcome.IsAccountCreated)
	require.True(t, outcome.IsContactCreated)
	require.Equal(t, 1, provider.RecordCount("Accounts"))
	require.Equal(t, 1, provider.RecordCount("Contacts"))
	contact := provider.Record("Contacts", outcome.ContactID)
	require.Equal(t, outcome.AccountID, contact["Account_Name"].(map[string]any)["id"], "the contact is linked to the account")
	upserts := provider.RequestsNamed("upsert")
	require.Len(t, upserts, 2)
	require.JSONEq(t, `{"data":[{"Account_Name":"Acme Corp"}],"duplicate_check_fields":["Account_Name"]}`, upserts[0].Body)
	require.JSONEq(t, `{"data":[{"Email":"jane@acme.example.com","Last_Name":"Smith","First_Name":"Jane","Account_Name":{"id":"`+outcome.AccountID+`"}}],
		"duplicate_check_fields":["Email"]}`, upserts[1].Body)
	queries := provider.RequestsNamed("coql")
	require.Len(t, queries, 2)
	require.Equal(t, "select Account_Name from Accounts where Account_Name = 'Acme Corp' order by id asc limit 0, 2", queries[0].SelectQuery)
	require.Equal(t, "select Deal_Name, Stage, Owner, Modified_Time from Deals where (Contact_Name = '"+outcome.ContactID+
		"' and Stage not in ('Closed Won', 'Closed Lost', 'Closed Lost to Competition')) order by Modified_Time desc, id asc limit 0, 1", queries[1].SelectQuery)
	require.Equal(t, url.Values{"module": {"Deals"}}, provider.LastRequest("fields").Query)
	require.Zero(t, provider.Count("update"))
}

func TestRepeatLeadAdvancesTheContactsNewestOpenDealAndLeavesDecoysUntouchedWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	accountID, contactID, dealID, decoys := seedJaneWithDeals(provider)
	before := map[string]map[string]any{}
	for _, decoy := range decoys {
		before[decoy] = provider.Record("Deals", decoy)
	}
	harness := newQualificationHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runQualification(t, "repeat-lead", integrationInput())
	require.Equal(t, PhaseDealAdvanced, outcome.Phase)
	require.Equal(t, accountID, outcome.AccountID)
	require.False(t, outcome.IsAccountCreated)
	require.Equal(t, contactID, outcome.ContactID, "the email matched without regard to case")
	require.False(t, outcome.IsContactCreated)
	require.Equal(t, dealID, outcome.DealID, "the newest open deal, not the newer closed deal or another contact's deal")
	require.Equal(t, "Proposal/Price Quote", outcome.DealStage)
	require.Equal(t, integrationOwnerID, outcome.DealOwnerID)
	require.JSONEq(t, `{"data":[{"Stage":"Proposal/Price Quote","Owner":{"id":"`+integrationOwnerID+`"}}]}`, provider.LastRequest("update").Body)
	require.Equal(t, url.Values{"fields": {"Deal_Name,Stage,Owner,Contact_Name,Modified_Time"}}, provider.LastRequest("get").Query)
	for _, decoy := range decoys {
		require.Equal(t, before[decoy], provider.Record("Deals", decoy), "deal %s must not be touched", decoy)
	}
	require.Equal(t, 1, provider.RecordCount("Accounts"))
	require.Equal(t, 2, provider.RecordCount("Contacts"))
	require.Equal(t, 1, provider.Count("upsert"), "the found account is not upserted")
}

func TestUnknownStageAndAmbiguousAccountCompleteWithoutWritingWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	provider.SeedRecord("Accounts", map[string]any{"Account_Name": "Acme Corp"}, seededAt)
	provider.SeedRecord("Accounts", map[string]any{"Account_Name": "acme corp"}, seededAt)
	harness := newQualificationHarness(t, provider, defaultRequestTimeout)

	input := integrationInput()
	input.Stage = "Won"
	outcome := harness.runQualification(t, "unknown-stage", input)
	require.Equal(t, PhaseUnknownStage, outcome.Phase)
	input.Stage = "Retired"
	outcome = harness.runQualification(t, "unused-stage", input)
	require.Equal(t, PhaseUnknownStage, outcome.Phase, "an unused picklist option is not offered")
	require.Zero(t, provider.Count("coql"))

	outcome = harness.runQualification(t, "ambiguous-account", integrationInput())
	require.Equal(t, PhaseAmbiguousAccount, outcome.Phase)
	require.Zero(t, provider.Count("upsert"))
	require.Zero(t, provider.Count("update"))
}

// TestSlowContactUpsertDispatchedTwiceKeepsOneContactWithRealDex is the duplicate-dispatch test: the
// first upsert inserts and answers after nine seconds, async Dex dispatches the Step again, and the
// second upsert finds the contact by Email and updates it.
func TestSlowContactUpsertDispatchedTwiceKeepsOneContactWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	provider.DelaysFirstUpsertOf = "Contacts"
	provider.SeedRecord("Accounts", map[string]any{"Account_Name": "Acme Corp"}, seededAt)
	harness := newQualificationHarness(t, provider, slowRequestTimeout)

	startedAt := time.Now()
	outcome := harness.runQualification(t, "slow-upsert", integrationInput())
	provider.WaitForDelayedRequests(t)
	require.Equal(t, PhaseNoOpenDeal, outcome.Phase)
	require.GreaterOrEqual(t, provider.Count("upsert"), 2, "Dex dispatched the upsert again past its local phase")
	require.Equal(t, 1, provider.RecordCount("Contacts"), "the repeated upsert updated the contact instead of creating a second")
	upserts := provider.RequestsNamed("upsert")
	for _, request := range upserts {
		require.Equal(t, upserts[0].Body, request.Body, "every dispatch sends the same record")
	}
	t.Logf("slow upsert: upserts=%d elapsed=%s isContactCreated=%t", len(upserts), time.Since(startedAt), outcome.IsContactCreated)
}

func TestSlowDealUpdateDispatchedTwiceSetsTheSameValuesWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	_, _, dealID, _ := seedJaneWithDeals(provider)
	provider.DelaysFirstUpdate = true
	harness := newQualificationHarness(t, provider, slowRequestTimeout)

	outcome := harness.runQualification(t, "slow-update", integrationInput())
	provider.WaitForDelayedRequests(t)
	require.Equal(t, PhaseDealAdvanced, outcome.Phase)
	require.GreaterOrEqual(t, provider.Count("update"), 2, "Dex dispatched the update again past its local phase")
	for _, request := range provider.RequestsNamed("update") {
		require.JSONEq(t, `{"data":[{"Stage":"Proposal/Price Quote","Owner":{"id":"`+integrationOwnerID+`"}}]}`, request.Body)
	}
	require.Equal(t, "Proposal/Price Quote", provider.Record("Deals", dealID)["Stage"])
}

func TestLostUpdateResponseIsRetriedWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	seedJaneWithDeals(provider)
	provider.LosesFirstUpdateResponse = true
	harness := newQualificationHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runQualification(t, "lost-update", integrationInput())
	require.Equal(t, PhaseDealAdvanced, outcome.Phase)
	require.Equal(t, 2, provider.Count("update"), "a lost response is retried; the update is safe to repeat")
}

func TestRateLimitedQueryWaitsForRetryAfterWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	provider.RateLimitsFirstCOQL = true
	harness := newQualificationHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runQualification(t, "rate-limited", integrationInput())
	require.Equal(t, PhaseNoOpenDeal, outcome.Phase)
	queries := provider.RequestsNamed("coql")
	require.Len(t, queries, 3)
	require.GreaterOrEqual(t, queries[1].At.Sub(queries[0].At), time.Second, "the retry waited for Retry-After")
}

func TestRejectedContactCompletesWithZohoCodesOnlyWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	provider.RejectsUpsertField = "Email"
	provider.SeedRecord("Accounts", map[string]any{"Account_Name": "Acme Corp"}, seededAt)
	harness := newQualificationHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runQualification(t, "rejected-contact", integrationInput())
	require.Equal(t, PhaseContactRejected, outcome.Phase)
	require.Equal(t, []crm.ProviderError{{Code: "INVALID_DATA", Field: "Email"}}, outcome.ProviderErrors)
	encoded, err := json.Marshal(outcome)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.Equal(t, 1, provider.Count("upsert"), "a record rejection is not retried")
}

func TestLockedDealFailsTheFlowWithoutZohoTextWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	seedJaneWithDeals(provider)
	provider.LocksUpdates = true
	harness := newQualificationHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startQualification(t, "locked-deal", integrationInput())

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional recordRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, integrationAccessToken)
	require.Equal(t, 1, provider.Count("update"))
	t.Logf("locked deal failure: %s", result.ErrorMessage)
}

func TestInvalidLeadFailsBeforeCallingZohoCRMWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	harness := newQualificationHarness(t, provider, defaultRequestTimeout)
	input := integrationInput()
	input.AccountName = "Macy's"
	flowID := harness.startQualification(t, "invalid", input)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.TotalRequests())
}

// TestRevokedAccessTokenIsRefreshedAtTheEUAccountsServerWithRealDex runs a refreshing credential source
// against the EU data center's real host names: Zoho CRM rejects the stored token at the first upsert,
// accounts.zoho.eu issues a new one with its api_domain, and the rejected upsert is sent once more.
func TestRevokedAccessTokenIsRefreshedAtTheEUAccountsServerWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	provider.RefreshedAccessToken, provider.RefreshToken, provider.RefreshedAPIDomain = integrationRefreshedToken, integrationRefreshToken, "https://www.zohoapis.eu"
	provider.RevokesTokenAtFirstUpsert = true
	expiresAt := time.Now().Add(30 * time.Minute)
	credentials := testsupport.NewRefreshingCredentialSource(crm.Credentials{
		AuthMethodID: crm.EUDataCenterAuthMethodID, OAuthClientID: "1000.ZOHOCRMINTEGRATIONCLIENT",
		OAuthClientSecret: sdkgo.NewSecretString("zoho-integration-secret"), AccessToken: sdkgo.NewSecretString(integrationAccessToken),
		RefreshToken: sdkgo.NewSecretString(integrationRefreshToken), APIDomain: "https://www.zohoapis.eu",
	}, &expiresAt)
	client, err := crm.New(crm.Config{}, credentials,
		crm.WithHTTPClient(provider.AccountsRoutingClient(defaultRequestTimeout, "accounts.zoho.eu", "www.zohoapis.eu")))
	require.NoError(t, err)
	connection, err := crm.NewConnection(client, sdkgo.ConnectionRef{Provider: "zoho", Name: ConnectionName})
	require.NoError(t, err)
	harness := newQualificationHarnessForConnection(t, connection)

	outcome := harness.runQualification(t, "refresh", integrationInput())
	require.Equal(t, PhaseNoOpenDeal, outcome.Phase)
	require.Equal(t, 1, provider.Count("token"), "one forced refresh at https://accounts.zoho.eu")
	require.Equal(t, 3, provider.Count("upsert"), "the 401 upsert applied nothing and was sent once more; then the contact upsert")
	require.Equal(t, 1, provider.RecordCount("Accounts"))
	require.Equal(t, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {integrationRefreshToken},
		"client_id": {"1000.ZOHOCRMINTEGRATIONCLIENT"}, "client_secret": {"zoho-integration-secret"},
	}, provider.LastRequest("token").Form)
	stored, _ := credentials.Current()
	require.Equal(t, crm.EUDataCenterAuthMethodID, stored.AuthMethodID)
	require.Equal(t, integrationRefreshedToken, stored.AccessToken.Reveal())
	require.Equal(t, integrationRefreshToken, stored.RefreshToken.Reveal(), "Zoho does not rotate refresh tokens")
	require.Equal(t, "https://www.zohoapis.eu", stored.APIDomain)
}

// qualificationHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type qualificationHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newQualificationHarness(t *testing.T, provider *fakecrm.Server, requestTimeout time.Duration) *qualificationHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "zoho", Name: ConnectionName}
	credentials := crm.Credentials{
		AuthMethodID: crm.USDataCenterAuthMethodID, OAuthClientID: "1000.ZOHOCRMINTEGRATIONCLIENT",
		OAuthClientSecret: sdkgo.NewSecretString("zoho-integration-secret"), AccessToken: sdkgo.NewSecretString(integrationAccessToken),
		RefreshToken: sdkgo.NewSecretString(integrationRefreshToken), APIDomain: "https://www.zohoapis.com",
	}
	providerClient, err := crm.New(crm.Config{}, sdkgo.StaticCredentialProvider[crm.Credentials]{reference: credentials},
		crm.WithAPIBaseURL(provider.URL+"/crm/v8"), crm.WithHTTPClient(&http.Client{Timeout: requestTimeout}))
	require.NoError(t, err)
	connection, err := crm.NewConnection(providerClient, reference)
	require.NoError(t, err)
	return newQualificationHarnessForConnection(t, connection)
}

func newQualificationHarnessForConnection(t *testing.T, connection crm.Connection) *qualificationHarness {
	t.Helper()
	harness := &qualificationHarness{flow: NewFlow(connection), serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	var err error
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker, harness.workerResult = worker, make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

func (harness *qualificationHarness) runQualification(t *testing.T, scenario string, input Input) LeadQualification {
	t.Helper()
	flowID := harness.startQualification(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome LeadQualification
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func (harness *qualificationHarness) startQualification(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "zoho-crm-lead-qualification-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *qualificationHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for {
		result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not close", flowID)
		return result
	}
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
