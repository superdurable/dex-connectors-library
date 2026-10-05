//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supportrequest

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	shortRequestTimeout = 500 * time.Millisecond
	// slowRequestTimeout outlasts the fake's nine-second responses and Dex's roughly seven-second async local phase.
	slowRequestTimeout = 20 * time.Second
	slowProviderDelay  = 9 * time.Second

	integrationDestination = "In progress"
	firstCreatedKey        = "ITH-5"
)

func TestNewRequestIsRaisedOnceForTheCustomerLabeledNotedRepliedAndMovedWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	record := runSupportFlow(t, ctx, harness, flow, "created", supportInput("Laptop will not boot"))
	require.Equal(t, PhaseHandled, record.Phase)
	require.Equal(t, janeAccountID, record.RequesterAccountID)
	require.False(t, record.IsExistingTicketReused, "the near-duplicate %s is not the same request", nearDuplicateKey)
	require.Equal(t, firstCreatedKey, record.Ticket.Key)
	require.Equal(t, "My laptop will not boot.\nError 0x7B.", record.Ticket.Description)
	require.Equal(t, []string{"Time to first response (running)", "Time to resolution (running)"}, record.SLANames)
	require.Equal(t, []string{"hardware"}, record.Labels)
	require.False(t, record.InternalNote.IsPublic)
	require.NotEmpty(t, record.InternalNote.CommentID)
	require.True(t, record.PublicReply.IsPublic)
	require.Equal(t, integrationDestination, record.Status.Name)

	request := provider.request(firstCreatedKey)
	require.Equal(t, janeAccountID, request.reporterAccountID, "the request was raised on the customer's behalf")
	require.Equal(t, "High", request.priorityName)
	require.Equal(t, []string{"hardware"}, request.labels)
	require.Equal(t, []bool{false, true}, request.commentVisibilities(), "an internal note, then a public reply")
	require.Equal(t, 1, provider.createCount("Laptop will not boot"))
	require.Equal(t, 1, provider.updateCount(firstCreatedKey))
	require.Equal(t, 1, provider.transitionPostCount(firstCreatedKey))
	require.Equal(t, []string{`project = "ITH" AND statusCategory in (2, 4) AND reporter in ("` + janeAccountID +
		`") AND summary ~ "\"Laptop will not boot\"" ORDER BY created DESC`}, provider.searchQueries())
}

func TestCustomersOpenRequestWithTheSameSummaryIsReusedWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	record := runSupportFlow(t, ctx, harness, flow, "reused", supportInput(openDuplicateSummary))
	require.Equal(t, PhaseHandled, record.Phase)
	require.True(t, record.IsExistingTicketReused)
	require.Equal(t, openDuplicateKey, record.Ticket.Key)
	require.Zero(t, record.CreateAttempts)
	require.Zero(t, provider.createCount(openDuplicateSummary))
	require.Equal(t, []bool{false, true}, provider.request(openDuplicateKey).commentVisibilities())
}

func TestUnknownRequesterCompletesWithoutRaisingARequestWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	input := supportInput("Monitor flickers")
	input.RequesterEmail = "jane@acme.example"
	record := runSupportFlow(t, ctx, harness, flow, "unknown-requester", input)
	require.Equal(t, PhaseRequesterNotFound, record.Phase)
	require.Empty(t, record.RequesterAccountID)
	require.Zero(t, provider.createCount("Monitor flickers"))
	require.Empty(t, provider.searchQueries())
}

func TestRejectedCreateCompletesWithTheFieldsAndErrorKeyWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	summary := markerReject + " Laptop will not boot"
	record := runSupportFlow(t, ctx, harness, flow, "rejected", supportInput(summary))
	require.Equal(t, PhaseRejected, record.Phase)
	require.Equal(t, []string{"customfield_10050"}, record.RejectedFieldIDs)
	require.Equal(t, "sd.request.create.field.required", record.ProviderErrorKey)
	require.Nil(t, record.Ticket)
	require.Equal(t, 1, provider.createCount(summary))
}

func TestRateLimitedCreateIsRetriedAfterRetryAfterWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	summary := markerRateLimit + " Docking station dead"
	record := runSupportFlow(t, ctx, harness, flow, "rate-limited", supportInput(summary))
	require.Equal(t, PhaseHandled, record.Phase)
	require.Equal(t, 1, record.CreateAttempts, "a 429 raised nothing, so Dex retries the same Step execution")
	attempts := provider.createTimesFor(summary)
	require.Len(t, attempts, 2)
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), time.Second, "the retry waits for Retry-After")
}

func TestCreateTimeoutIsReconciledWithoutRaisingAgainWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)
	summary := markerCreateTimeout + " Badge does not open door"
	flowID := startSupportFlow(t, ctx, harness, flow, "timeout", supportInput(summary))

	uncertain := waitForPhase(t, ctx, harness, flow, flowID, PhaseNeedsReconciliation)
	require.NotNil(t, uncertain.UncertainCreate)
	require.Equal(t, sdkgo.FailureTransport, uncertain.UncertainCreate.FailureKind)
	require.NotEmpty(t, uncertain.UncertainCreate.CallID)
	require.Equal(t, 1, provider.createCount(summary), "a timeout after dispatch is never retried")

	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmCreatedTicket, ConfirmCreatedTicketInput{IssueKey: "ITH-99"}, nil))
	waitForReconciliationNote(t, ctx, harness, flow, flowID, NoteReportedTicketMissing)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmCreatedTicket, ConfirmCreatedTicketInput{IssueKey: nearDuplicateKey}, nil))
	waitForReconciliationNote(t, ctx, harness, flow, flowID, NoteReportedTicketMismatch)

	createdKey := provider.keyForSummary(summary)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmCreatedTicket, ConfirmCreatedTicketInput{IssueKey: strings.ToLower(createdKey)}, nil))
	record := waitForFlowOutput(t, ctx, harness, flowID)
	require.Equal(t, PhaseHandled, record.Phase)
	require.Equal(t, createdKey, record.Ticket.Key)
	require.Nil(t, record.UncertainCreate)
	require.Equal(t, 1, record.CreateAttempts)
	require.Equal(t, 1, provider.createCount(summary), "reconciliation adopted the existing request")
}

func TestServerErrorIsRaisedAgainOnlyAfterOperatorApprovalWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)
	summary := markerServerError + " Keyboard keys stick"
	flowID := startSupportFlow(t, ctx, harness, flow, "server-error", supportInput(summary))

	uncertain := waitForPhase(t, ctx, harness, flow, flowID, PhaseNeedsReconciliation)
	require.Equal(t, sdkgo.FailureAvailability, uncertain.UncertainCreate.FailureKind)
	require.Equal(t, 1, provider.createCount(summary))

	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ApproveTicketCreateRetry, nil, nil))
	record := waitForFlowOutput(t, ctx, harness, flowID)
	require.Equal(t, PhaseHandled, record.Phase)
	require.Equal(t, 2, record.CreateAttempts)
	require.Equal(t, 2, provider.createCount(summary), "only the approved retry sent a second create")
}

// TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex guards sync durability: async would resend a
// create outlasting Dex's seven-second local phase.
func TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, slowRequestTimeout)
	ctx := integrationContext(t)

	summary := markerSlowCreate + " Phone will not pair"
	record := runSupportFlow(t, ctx, harness, flow, "slow-create", supportInput(summary))
	require.Equal(t, PhaseHandled, record.Phase)
	require.Equal(t, 1, provider.createCount(summary), "one Step execution dispatches one create request")
}

// TestSlowPublicReplyIsSentOnceUnderSyncDurabilityWithRealDex proves the customer gets one reply when
// Jira Service Management answers after the async local phase would end.
func TestSlowPublicReplyIsSentOnceUnderSyncDurabilityWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, slowRequestTimeout)
	ctx := integrationContext(t)

	summary := markerSlowReply + " Shared drive missing"
	record := runSupportFlow(t, ctx, harness, flow, "slow-reply", supportInput(summary))
	require.Equal(t, PhaseHandled, record.Phase)
	require.NotEmpty(t, record.PublicReply.CommentID)
	require.Equal(t, []bool{false, true}, provider.request(provider.keyForSummary(summary)).commentVisibilities(),
		"one internal note and exactly one public reply")
}

// TestSlowLabelUpdateBackupAttemptWritesOnceWithRealDex proves async updateTicket is safe: the backup
// attempt reads first and finds the change applied.
func TestSlowLabelUpdateBackupAttemptWritesOnceWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, slowRequestTimeout)
	ctx := integrationContext(t)

	summary := markerSlowLabels + " Email signature wrong"
	record := runSupportFlow(t, ctx, harness, flow, "slow-labels", supportInput(summary))
	require.Equal(t, PhaseHandled, record.Phase)
	issueKey := provider.keyForSummary(summary)
	require.Equal(t, []string{"hardware"}, provider.request(issueKey).labels)
	require.Equal(t, 1, provider.updateCount(issueKey), "the backup attempt wrote nothing")
	t.Logf("slow labels: label reads=%d writes=%d", provider.labelReadCount(issueKey), provider.updateCount(issueKey))
}

// TestSlowTransitionBackupAttemptMovesTheRequestOnceWithRealDex proves async transitionTicket is safe: a
// backup attempt reads first and finds the transition made.
func TestSlowTransitionBackupAttemptMovesTheRequestOnceWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, slowRequestTimeout)
	ctx := integrationContext(t)

	summary := markerSlowTransition + " Wi-Fi password expired"
	record := runSupportFlow(t, ctx, harness, flow, "slow-transition", supportInput(summary))
	require.Equal(t, PhaseHandled, record.Phase)
	require.Equal(t, integrationDestination, record.Status.Name)
	require.Equal(t, 1, provider.transitionPostCount(provider.keyForSummary(summary)), "Jira saw one transition request")
}

func TestUnavailableTransitionCompletesWithTheOfferedTransitionsWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	input := supportInput("Headset crackles")
	input.DestinationStatusName = "Closed"
	record := runSupportFlow(t, ctx, harness, flow, "unavailable", input)
	require.Equal(t, PhaseTransitionUnavailable, record.Phase)
	require.Equal(t, "Waiting for support", record.Status.Name)
	require.Equal(t, []string{"Start work -> In progress", "Resolve this issue -> Resolved"}, record.AvailableTransitionNames)
	require.Zero(t, provider.transitionPostCount(record.Ticket.Key))
}

func TestInternalNoteTimeoutIsRecordedAndNeverResentWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	summary := markerNoteTimeout + " Printer offline"
	record := runSupportFlow(t, ctx, harness, flow, "note-timeout", supportInput(summary))
	require.Equal(t, PhaseHandled, record.Phase)
	require.True(t, record.InternalNote.IsOutcomeUnknown)
	require.Empty(t, record.InternalNote.CommentID)
	require.Equal(t, []bool{false, true}, provider.request(provider.keyForSummary(summary)).commentVisibilities(),
		"the uncertain internal note is never re-sent, and the public reply still follows")
	require.Equal(t, integrationDestination, record.Status.Name)
}

func TestBlankCloudIDUsesTheOnlyGrantedSiteWithRealDex(t *testing.T) {
	provider, flow, harness := newSupportIntegrationHarness(t, "", shortRequestTimeout)
	ctx := integrationContext(t)

	record := runSupportFlow(t, ctx, harness, flow, "blank-site", supportInput("Projector has no signal"))
	require.Equal(t, PhaseHandled, record.Phase)
	require.Equal(t, 1, provider.accessibleResourcesCount(), "the Worker's client caches the resolved site")
}

func supportInput(summary string) Input {
	return Input{
		RequesterEmail: "jane@acme.example.com", Summary: summary, Description: "My laptop will not boot.\nError 0x7B.",
		ServiceDeskID: "10", ProjectKey: "ITH", RequestTypeID: "25", Labels: []string{"hardware"}, PriorityName: "High",
		InternalNote: "Check the warranty first.", PublicReply: "We are looking into it.", DestinationStatusName: integrationDestination,
	}
}

func runSupportFlow(t *testing.T, ctx context.Context, harness *supportIntegrationHarness, flow *Flow, scenario string, input Input) SupportRequest {
	t.Helper()
	return waitForFlowOutput(t, ctx, harness, startSupportFlow(t, ctx, harness, flow, scenario, input))
}

func startSupportFlow(t *testing.T, ctx context.Context, harness *supportIntegrationHarness, flow *Flow, scenario string, input Input) string {
	t.Helper()
	flowID := "jsm-support-request-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func waitForFlowOutput(t *testing.T, ctx context.Context, harness *supportIntegrationHarness, flowID string) SupportRequest {
	t.Helper()
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s", flowID)
	var record SupportRequest
	require.NoError(t, result.DecodeSingleOutput(&record))
	return record
}

func waitForPhase(t *testing.T, ctx context.Context, harness *supportIntegrationHarness, flow *Flow, flowID string, phase string) SupportRequest {
	t.Helper()
	return waitForSupportRequest(t, ctx, harness, flow, flowID, func(record SupportRequest) bool { return record.Phase == phase })
}

func waitForReconciliationNote(t *testing.T, ctx context.Context, harness *supportIntegrationHarness, flow *Flow, flowID string, note string) SupportRequest {
	t.Helper()
	return waitForSupportRequest(t, ctx, harness, flow, flowID, func(record SupportRequest) bool {
		return record.Phase == PhaseNeedsReconciliation && record.ReconciliationNote == note
	})
}

func waitForSupportRequest(t *testing.T, ctx context.Context, harness *supportIntegrationHarness, flow *Flow, flowID string, isExpected func(SupportRequest) bool) SupportRequest {
	t.Helper()
	var record SupportRequest
	require.Eventually(t, func() bool {
		record = SupportRequest{}
		return harness.client.InvokeRPC(ctx, flowID, flow.GetSupportRequest, nil, &record) == nil && isExpected(record)
	}, 30*time.Second, 100*time.Millisecond, "Flow %s last record %+v", flowID, record)
	return record
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

type supportIntegrationHarness struct {
	cache        *blobcache.Cache
	worker       *dex.Worker
	workerResult chan error
	client       *dex.Client
}

func newSupportIntegrationHarness(t *testing.T, cloudID string, requestTimeout time.Duration) (*fakeServiceDesk, *Flow, *supportIntegrationHarness) {
	t.Helper()
	provider := newFakeServiceDesk(t)
	reference := sdkgo.ConnectionRef{Provider: "atlassian", Name: ConnectionName}
	providerClient, err := jiraservicemanagement.New(
		jiraservicemanagement.Config{CloudID: cloudID, Endpoint: provider.URL},
		sdkgo.StaticCredentialProvider[jiraservicemanagement.Credentials]{reference: {
			OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
			AccessToken: sdkgo.NewSecretString(integrationAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
		}},
		jiraservicemanagement.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := jiraservicemanagement.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, DeskSelection{}, RequestTypeSelection{})
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	harness := &supportIntegrationHarness{cache: cache, workerResult: make(chan error, 1)}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	go func() { harness.workerResult <- harness.worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return provider, flow, harness
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
