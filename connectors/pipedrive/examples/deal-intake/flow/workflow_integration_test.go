//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dealintake

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAPIToken       = "pipedrive-integration-api-token"
	integrationClientID       = "pipedrive-integration-client"
	integrationClientSecret   = "pipedrive-integration-client-secret"
	integrationAccessToken    = "v1u:pipedrive-integration-access"
	integrationRefreshedToken = "v1u:pipedrive-integration-refreshed"
	integrationRefreshToken   = "1:1:pipedrive-integration-refresh"
	integrationEmail          = "jane@acme.example.com"

	ownerID    = 7
	pipelineID = 1
	stageID    = 3

	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second
)

var integrationSettings = Settings{
	LeadOwner: LeadOwnerConfiguration{OwnerID: strconv.Itoa(ownerID)},
	Deal: DealConfiguration{
		PipelineID: strconv.Itoa(pipelineID), StageID: strconv.Itoa(stageID), SourceObjectType: "deals", SourceFieldKey: testSourceFieldKey,
	},
}

func integrationLead() Input {
	return Input{Email: integrationEmail, Name: "Jane Smith", Organization: "Acme Corp", DealTitle: "Acme platform deal", Source: "webinar"}
}

func TestNewLeadCreatesOnePersonAndOneDealWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	acme := provider.seedOrganization("Acme Corp")
	provider.seedOrganization("Acme Corp (EU)")
	decoyPerson := provider.seedPerson("Jane Smith-Okafor", "jane.smith@acme.example.com", acme)
	harness := newIntakeHarness(t, provider, defaultRequestTimeout)

	intake := harness.runIntake(t, "new-lead", integrationLead())
	require.Equal(t, PhaseCompleted, intake.Phase)
	require.Equal(t, strconv.Itoa(acme), intake.OrganizationID, "only the exact name links; the EU sibling is a decoy")
	require.Equal(t, 1, intake.OrganizationMatches)
	require.True(t, intake.IsPersonCreated)
	require.Equal(t, DealActionCreated, intake.DealAction)
	require.Equal(t, strconv.Itoa(stageID), intake.StageID)

	person := provider.person(atoi(intake.PersonID))
	require.Equal(t, fakePerson{id: person.id, name: "Jane Smith", email: integrationEmail, organizationID: acme, ownerID: ownerID, updateTime: person.updateTime}, person)
	deal := provider.deal(atoi(intake.DealID))
	require.Equal(t, "Acme platform deal", deal.title)
	require.Equal(t, []int{person.id, acme, pipelineID, stageID, ownerID}, []int{deal.personID, deal.organizationID, deal.pipelineID, deal.stageID, deal.ownerID})
	require.JSONEq(t, `"webinar"`, string(deal.customFields[testSourceFieldKey]))
	require.Equal(t, "Jane Smith-Okafor", provider.person(decoyPerson).name, "the near-duplicate person is untouched")
	require.Equal(t, 2, provider.personCount())
	require.Equal(t, 1, provider.dealCount())
	require.Equal(t, 1, provider.count("dealRead"))
}

func TestRepeatLeadAdvancesItsOpenDealAndLeavesDecoysUntouchedWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	acme := provider.seedOrganization("Acme Corp")
	existing := provider.seedPerson("J. Smith", integrationEmail, acme)
	openDeal := provider.seedDeal("Acme renewal", existing, pipelineID, 1, "open")
	wonDeal := provider.seedDeal("Acme renewal", existing, pipelineID, 5, "won")
	otherPipelineDeal := provider.seedDeal("Acme partner deal", existing, 2, 8, "open")
	harness := newIntakeHarness(t, provider, defaultRequestTimeout)

	intake := harness.runIntake(t, "repeat-lead", integrationLead())
	require.Equal(t, PhaseCompleted, intake.Phase)
	require.False(t, intake.IsPersonCreated)
	require.Equal(t, strconv.Itoa(existing), intake.PersonID)
	require.Equal(t, DealActionAdvanced, intake.DealAction)
	require.Equal(t, strconv.Itoa(openDeal), intake.DealID)
	require.Equal(t, "1", intake.PreviousStageID)
	require.Equal(t, stageID, provider.deal(openDeal).stageID)
	require.Equal(t, ownerID, provider.deal(openDeal).ownerID)
	require.Equal(t, 5, provider.deal(wonDeal).stageID, "the won deal with the same title is untouched")
	require.Equal(t, 8, provider.deal(otherPipelineDeal).stageID, "the open deal in another pipeline is untouched")
	require.Equal(t, "Jane Smith", provider.person(existing).name)
	require.Equal(t, 1, provider.personCount())
	require.Zero(t, provider.count("dealCreate"))
	require.Zero(t, provider.count("personCreate"))
}

// TestSlowPersonCreateIsSentOnceWithRealDex checks that sync durability dispatches no second attempt during an in-flight create.
func TestSlowPersonCreateIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	provider.delaysFirstPersonCreate = true
	harness := newIntakeHarness(t, provider, slowRequestTimeout)

	startedAt := time.Now()
	intake := harness.runIntake(t, "slow-person", integrationLead())
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, PhaseCompleted, intake.Phase)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("personCreate"), "no second dispatch while the first was in flight")
	require.Equal(t, 1, provider.count("personSearch"))
	require.Equal(t, 1, provider.personCount())
}

func TestSlowDealCreateIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	provider.delaysFirstDealCreate = true
	harness := newIntakeHarness(t, provider, slowRequestTimeout)

	intake := harness.runIntake(t, "slow-deal", integrationLead())
	require.Equal(t, PhaseCompleted, intake.Phase)
	require.Equal(t, DealActionCreated, intake.DealAction)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("dealCreate"), "no second dispatch while the first was in flight")
	require.Equal(t, 1, provider.dealCount())
}

// TestSlowDealUpdateIsSafeToRepeatWithRealDex lets async Dex dispatch the update again; both attempts write the same values.
func TestSlowDealUpdateIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	existing := provider.seedPerson("Jane Smith", integrationEmail, 0)
	openDeal := provider.seedDeal("Acme renewal", existing, pipelineID, 1, "open")
	provider.delaysFirstDealUpdate = true
	harness := newIntakeHarness(t, provider, slowRequestTimeout)

	intake := harness.runIntake(t, "slow-update", integrationLead())
	require.Equal(t, DealActionAdvanced, intake.DealAction)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("dealUpdate"), 2, "Dex dispatched the update again past its local phase")
	for _, request := range provider.requestsNamed("dealUpdate") {
		require.JSONEq(t, `{"stage_id":3,"owner_id":7}`, request.body, "every dispatch writes the same absolute values")
	}
	require.Equal(t, stageID, provider.deal(openDeal).stageID)
	require.Equal(t, 1, provider.dealCount())
	t.Logf("slow update: updates=%d", provider.count("dealUpdate"))
}

func TestLostDealCreateResponseSelectsUncertainAndIsNeverResentWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	provider.losesFirstDealCreateResponse = true
	harness := newIntakeHarness(t, provider, defaultRequestTimeout)

	intake := harness.runIntake(t, "lost-deal", integrationLead())
	require.Equal(t, PhaseNeedsReview, intake.Phase)
	require.Equal(t, createDealStepType, intake.ReviewStep)
	require.Equal(t, sdkgo.UncertainBranchID, intake.ReviewBranch)
	require.Equal(t, "Pipedrive request failed before a response arrived", intake.ReviewDetail.Message)
	require.Equal(t, 1, provider.count("dealCreate"), "an unconfirmed create is never resent")
	require.Equal(t, 1, provider.dealCount(), "Pipedrive did create the deal, which a person must now find")
}

func TestLostPersonCreateResponseIsReadBackInsteadOfCreatedAgainWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	provider.losesFirstPersonCreateResponse = true
	harness := newIntakeHarness(t, provider, defaultRequestTimeout)

	intake := harness.runIntake(t, "lost-person", integrationLead())
	require.Equal(t, PhaseCompleted, intake.Phase)
	require.False(t, intake.IsPersonCreated, "the retry found the person the lost attempt created")
	require.Equal(t, 1, provider.count("personCreate"))
	require.Equal(t, 2, provider.count("personSearch"), "the retry searched the email again")
	require.Equal(t, 1, provider.count("personUpdate"))
	require.Equal(t, 1, provider.personCount())
}

// TestLostWorkerDuringDealCreateSelectsUncertainWithoutResendingWithRealDex replaces the Worker while
// Pipedrive holds the create; the next attempt finds the dispatch checkpoint and sends nothing.
func TestLostWorkerDuringDealCreateSelectsUncertainWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	provider.holdsFirstDealCreate = make(chan struct{})
	harness := newIntakeHarness(t, provider, slowRequestTimeout)
	flowID := harness.startIntake(t, "lost-worker", integrationLead())
	require.Eventually(t, func() bool { return provider.count("dealCreate") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Pipedrive")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(provider.holdsFirstDealCreate)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var intake DealIntake
	require.NoError(t, result.DecodeSingleOutput(&intake))
	require.Equal(t, PhaseNeedsReview, intake.Phase)
	require.Equal(t, sdkgo.UncertainBranchID, intake.ReviewBranch)
	require.Equal(t, "an earlier attempt of this Step may have sent the create, so it is not sent again", intake.ReviewDetail.Message,
		"the new Worker's attempt found the checkpoint the lost attempt recorded")
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("dealCreate"), "the attempt on the new Worker did not resend the create")
}

func TestBurstLimitedDealCreateWaitsAndCreatesOneDealWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	provider.rateLimitsFirstDealCreate = true
	harness := newIntakeHarness(t, provider, defaultRequestTimeout)

	intake := harness.runIntake(t, "burst-limit", integrationLead())
	require.Equal(t, PhaseCompleted, intake.Phase, "a 429 clears the dispatch checkpoint, so the retry may send")
	requests := provider.requestsNamed("dealCreate")
	require.Len(t, requests, 2)
	require.GreaterOrEqual(t, requests[1].at.Sub(requests[0].at), 2*time.Second, "the retry waited out Pipedrive's burst window")
	require.Equal(t, 1, provider.dealCount())
}

func TestSharedEmailCompletesForReviewWithoutWritingWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	first := provider.seedPerson("Jane Smith", integrationEmail, 0)
	second := provider.seedPerson("Jane S.", "JANE@acme.example.com", 0)
	harness := newIntakeHarness(t, provider, defaultRequestTimeout)

	intake := harness.runIntake(t, "shared-email", integrationLead())
	require.Equal(t, PhaseNeedsReview, intake.Phase)
	require.Equal(t, upsertLeadPersonStepType, intake.ReviewStep)
	require.Equal(t, pipedrive.UpsertObjectBranchMultipleMatches, intake.ReviewBranch)
	require.Equal(t, sdkgo.FailureConflict, intake.ReviewDetail.Kind)
	require.Zero(t, provider.count("personCreate")+provider.count("personUpdate"))
	require.Zero(t, provider.count("dealList"))
	require.Equal(t, "Jane S.", provider.person(second).name)
	require.Equal(t, "Jane Smith", provider.person(first).name)
}

func TestRejectedDealCompletesForReviewWithoutPipedriveTextWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	provider.rejectsDealCreate = true
	harness := newIntakeHarness(t, provider, defaultRequestTimeout)

	intake := harness.runIntake(t, "rejected-deal", integrationLead())
	require.Equal(t, PhaseNeedsReview, intake.Phase)
	require.Equal(t, pipedrive.CreateObjectBranchProviderRejected, intake.ReviewBranch)
	require.Equal(t, sdkgo.FailureValidation, intake.ReviewDetail.Kind)
	require.NotContains(t, intake.ReviewDetail.Message, "SENTINEL")
	require.Zero(t, provider.dealCount())
}

// TestExpiredOAuthTokenIsRefreshedAndRequestsUseTheAPIDomainWithRealDex checks that requests follow the refreshed token's api_domain.
func TestExpiredOAuthTokenIsRefreshedAndRequestsUseTheAPIDomainWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	provider.acceptedBearer = integrationAccessToken
	expired := time.Now().Add(-time.Minute)
	credentials := testsupport.NewRefreshingCredentialSource(pipedrive.Credentials{
		AuthMethodID: pipedrive.OAuthAuthMethodID, OAuthClientID: integrationClientID,
		OAuthClientSecret: sdkgo.NewSecretString(integrationClientSecret), AccessToken: sdkgo.NewSecretString(integrationAccessToken),
		RefreshToken: sdkgo.NewSecretString(integrationRefreshToken), APIDomain: "https://stale-company.pipedrive.com",
	}, &expired)
	client, err := pipedrive.New(pipedrive.Config{}, credentials, pipedrive.WithHTTPClient(provider.routingClient(t, defaultRequestTimeout)))
	require.NoError(t, err)
	connection, err := pipedrive.NewConnection(client, sdkgo.ConnectionRef{Provider: "pipedrive", Name: ConnectionName})
	require.NoError(t, err)
	harness := newIntakeHarnessForConnection(t, connection)

	intake := harness.runIntake(t, "oauth-refresh", integrationLead())
	require.Equal(t, PhaseCompleted, intake.Phase)
	require.Equal(t, 1, provider.count("token"), "one refresh serves every later Step")
	current, expiresAt := credentials.Current()
	require.Equal(t, integrationRefreshedToken, current.AccessToken.Reveal())
	require.Equal(t, provider.URL, current.APIDomain)
	require.Equal(t, integrationRefreshToken, current.RefreshToken.Reveal())
	require.True(t, expiresAt.After(time.Now().Add(50*time.Minute)))
	require.Equal(t, 1, provider.dealCount())
}

func TestInvalidLeadFailsBeforeCallingPipedriveWithRealDex(t *testing.T) {
	provider := newFakePipedrive(t)
	harness := newIntakeHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startIntake(t, "invalid-lead", Input{Email: "Jane <jane@acme.example.com>", Name: "Jane", DealTitle: "Deal"})
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "email must be one plain email address")
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Empty(t, provider.counts)
}

// intakeHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type intakeHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newIntakeHarness(t *testing.T, provider *fakePipedrive, requestTimeout time.Duration) *intakeHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "pipedrive", Name: ConnectionName}
	client, err := pipedrive.New(pipedrive.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[pipedrive.Credentials]{
		reference: {AuthMethodID: pipedrive.APITokenAuthMethodID, APIToken: sdkgo.NewSecretString(integrationAPIToken)},
	}, pipedrive.WithHTTPClient(&http.Client{Timeout: requestTimeout}))
	require.NoError(t, err)
	connection, err := pipedrive.NewConnection(client, reference)
	require.NoError(t, err)
	return newIntakeHarnessForConnection(t, connection)
}

func newIntakeHarnessForConnection(t *testing.T, connection pipedrive.Connection) *intakeHarness {
	t.Helper()
	flow, err := NewFlow(connection, &integrationSettings)
	require.NoError(t, err)
	harness := &intakeHarness{flow: flow, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.startWorker(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

func (harness *intakeHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	harness.worker, harness.workerResult = worker, workerResult
}

// replaceWorker force-stops the Worker without draining its handlers, like a crash, then starts a new one.
func (harness *intakeHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *intakeHarness) runIntake(t *testing.T, scenario string, input Input) DealIntake {
	t.Helper()
	flowID := harness.startIntake(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var intake DealIntake
	require.NoError(t, result.DecodeSingleOutput(&intake))
	encoded, err := json.Marshal(intake)
	require.NoError(t, err)
	for _, secret := range []string{integrationAPIToken, integrationAccessToken, integrationRefreshedToken, integrationRefreshToken, integrationClientSecret} {
		require.NotContains(t, string(encoded), secret, "no credential reaches the Flow output")
	}
	return intake
}

func (harness *intakeHarness) startIntake(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "pipedrive-deal-intake-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *intakeHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
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
