//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package policypublish

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
	"github.com/superdurable/dex-connectors-library/connectors/google/docs"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs/internal/fakegoogledocs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAccessToken = "docs-integration-token"
	templateDocumentID     = "policyTemplate1"
	destinationFolderID    = "fld_policies"
	// slowProviderDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowProviderDelay = 9 * time.Second
	// providerRequestTimeout lets a slow fake request finish inside the 30-second Execute timeout.
	providerRequestTimeout = 20 * time.Second
	// readsWithoutDuplicates is one read each for the template, the draft, the fill, the stamp, and the read-back.
	readsWithoutDuplicates = 5
)

var integrationPlaceholders = []docs.PlaceholderReplacement{
	{Placeholder: "{{effectiveDate}}", Text: "2026-10-01"},
	{Placeholder: "{{companyName}}", Text: "Acme"},
	{Placeholder: "{{approvalThreshold}}", Text: "USD 500"},
	{Placeholder: "{{policyOwner}}", Text: "finance@acme.example"},
}

var publishedPolicyParagraphs = []fakegoogledocs.Paragraph{
	{Text: "Refund Policy", NamedStyleType: "HEADING_1"},
	{Text: "Effective 2026-10-01 for Acme customers."},
	{Text: "Approvals", NamedStyleType: "HEADING_2"},
	{Text: "Refunds above USD 500 need a manager.", IsBullet: true},
	{Text: "Refunds are paid within 5 business days.", IsBullet: true},
	{Text: "Questions go to finance@acme.example."},
}

const publishedPolicyMarkdown = "# Refund Policy\n\nEffective 2026-10-01 for Acme customers.\n\n## Approvals\n\n" +
	"- Refunds above USD 500 need a manager.\n- Refunds are paid within 5 business days.\n\nQuestions go to finance@acme.example.\n\n"

func TestPolicyPublishFillsTheTemplateAndReadsItBackWithRealDex(t *testing.T) {
	provider := newPolicyProvider(t)
	harness := newPublishHarness(t, provider)

	outcome := harness.runPublish(t, "published", publishInput())

	require.Equal(t, StatusPublished, outcome.Status)
	require.Equal(t, templateDocumentID, outcome.TemplateDocumentID)
	require.Equal(t, "rev-policyTemplate1-1", outcome.TemplateRevisionID)
	require.NotNil(t, outcome.Document)
	require.Equal(t, []string{destinationFolderID}, outcome.Document.Parents)
	require.False(t, outcome.IsCreationFromEarlierAttempt)
	require.False(t, outcome.WasFillAlreadyApplied)
	require.False(t, outcome.WasStampAlreadyApplied)
	require.Empty(t, outcome.RemainingPlaceholders)
	stamp := PublicationStampText(time.Now().UTC().Format(publicationStampDateLayout), "rev-policyTemplate1-1")
	require.Equal(t, publishedPolicyMarkdown+stamp, outcome.PublishedText)
	require.Equal(t, "rev-"+outcome.Document.DocumentID+"-3", outcome.PublishedRevisionID, "created, filled, then stamped")
	requirePublishedDocument(t, provider, outcome)
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountCreatesReceived))
	require.Equal(t, 2, provider.Count(fakegoogledocs.CountBatchUpdatesApplied))
	require.Equal(t, readsWithoutDuplicates, provider.Count(fakegoogledocs.CountDocumentReads))
}

// createDocument has no provider idempotency key, so it runs sync: Dex never dispatches it again while it is in flight.
func TestPolicyPublishSlowCreateIsSentOnceWithRealDex(t *testing.T) {
	provider := newPolicyProvider(t)
	provider.DelayFirstCreate(slowProviderDelay)
	harness := newPublishHarness(t, provider)

	startedAt := time.Now()
	outcome := harness.runPublish(t, "slow-create", publishInput())
	require.GreaterOrEqual(t, time.Since(startedAt), slowProviderDelay)
	require.Equal(t, StatusPublished, outcome.Status)
	provider.WaitForDelayedRequests(t, 2*slowProviderDelay)
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountCreatesReceived), "no second dispatch while the first was in flight")
	require.Len(t, provider.CreatedDocumentIDs(), 1)
}

func TestPolicyPublishRetriedCreateReusesTheDocumentAnEarlierAttemptCreatedWithRealDex(t *testing.T) {
	provider := newPolicyProvider(t)
	provider.QueueCreateResponses(fakegoogledocs.CreateResponseCreatedThenRateLimited)
	harness := newPublishHarness(t, provider)

	outcome := harness.runPublish(t, "retried-create", publishInput())
	require.Equal(t, StatusPublished, outcome.Status)
	require.True(t, outcome.IsCreationFromEarlierAttempt, "the retry found the document by its private app property")
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountCreatesReceived))
	require.Equal(t, []string{outcome.Document.DocumentID}, provider.CreatedDocumentIDs())
	requirePublishedDocument(t, provider, outcome)
}

func TestPolicyPublishLostCreateResponseCompletesUncertainWithoutResendingWithRealDex(t *testing.T) {
	provider := newPolicyProvider(t)
	provider.QueueCreateResponses(fakegoogledocs.CreateResponseCreatedThenServerError)
	harness := newPublishHarness(t, provider)

	outcome := harness.runPublish(t, "lost-create", publishInput())
	require.Equal(t, StatusCreationUncertain, outcome.Status)
	require.Equal(t, "provider create outcome is unknown", outcome.ReviewDetail)
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountCreatesReceived))
	require.Zero(t, provider.Count(fakegoogledocs.CountBatchUpdatesReceived))
}

func TestPolicyPublishLostWorkerDuringCreateIsNeverResentWithRealDex(t *testing.T) {
	provider := newPolicyProvider(t)
	hold := make(chan struct{})
	provider.HoldFirstCreate(hold)
	provider.HideCreatedDocumentsFromLookup()
	harness := newPublishHarness(t, provider)

	flowID := harness.startPublish(t, "lost-worker", publishInput())
	require.Eventually(t, func() bool { return provider.Count(fakegoogledocs.CountCreatesReceived) == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Google")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(hold)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, StatusCreationUncertain, outcome.Status)
	require.Equal(t, "an earlier attempt of this Step may have created the document, and the duplicate lookup does not show it yet", outcome.ReviewDetail,
		"the new Worker's attempt found the heartbeat checkpoint the lost attempt recorded")
	provider.WaitForDelayedRequests(t, 2*slowProviderDelay)
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountCreatesReceived), "the attempt on the new Worker did not resend the create")
}

// A fill that answers late is dispatched again by async Dex; the second dispatch finds its own text.
func TestPolicyPublishSlowFillResponseAppliesOnceWithRealDex(t *testing.T) {
	provider := newPolicyProvider(t)
	provider.SetWriteBehavior(fakegoogledocs.WriteKindReplace, fakegoogledocs.WriteBehavior{DelayAfterApplying: slowProviderDelay})
	harness := newPublishHarness(t, provider)

	outcome := harness.runPublish(t, "slow-fill-response", publishInput())
	require.Equal(t, StatusPublished, outcome.Status)
	provider.WaitForDelayedRequests(t, 2*slowProviderDelay)
	require.Greater(t, provider.Count(fakegoogledocs.CountDocumentReads), readsWithoutDuplicates, "Dex dispatched the slow fill again")
	require.Equal(t, 2, provider.Count(fakegoogledocs.CountBatchUpdatesApplied), "one fill and one stamp")
	requirePublishedDocument(t, provider, outcome)
	t.Logf("slow fill response: reads=%d batches=%d wasFillAlreadyApplied=%t", provider.Count(fakegoogledocs.CountDocumentReads),
		provider.Count(fakegoogledocs.CountBatchUpdatesReceived), outcome.WasFillAlreadyApplied)
}

// Both dispatches read the same revision before either writes. Google applies only the first batch that reaches it.
func TestPolicyPublishConcurrentFillDispatchesApplyOnceWithRealDex(t *testing.T) {
	provider := newPolicyProvider(t)
	provider.SetWriteBehavior(fakegoogledocs.WriteKindReplace, fakegoogledocs.WriteBehavior{DelayBeforeApplying: slowProviderDelay})
	harness := newPublishHarness(t, provider)

	outcome := harness.runPublish(t, "concurrent-fill", publishInput())
	require.Equal(t, StatusPublished, outcome.Status)
	provider.WaitForDelayedRequests(t, 2*slowProviderDelay)
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountStaleRevisionRejected), "the delayed batch carried a revision that was no longer current")
	require.Equal(t, 2, provider.Count(fakegoogledocs.CountBatchUpdatesApplied), "one fill and one stamp")
	requirePublishedDocument(t, provider, outcome)
}

func TestPolicyPublishSlowStampAppendsOnceWithRealDex(t *testing.T) {
	provider := newPolicyProvider(t)
	provider.SetWriteBehavior(fakegoogledocs.WriteKindAppend, fakegoogledocs.WriteBehavior{DelayAfterApplying: slowProviderDelay})
	harness := newPublishHarness(t, provider)

	outcome := harness.runPublish(t, "slow-stamp", publishInput())
	require.Equal(t, StatusPublished, outcome.Status)
	provider.WaitForDelayedRequests(t, 2*slowProviderDelay)
	require.Greater(t, provider.Count(fakegoogledocs.CountDocumentReads), readsWithoutDuplicates, "Dex dispatched the slow append again")
	require.Equal(t, 2, provider.Count(fakegoogledocs.CountBatchUpdatesApplied), "one fill and one stamp")
	requirePublishedDocument(t, provider, outcome)
}

func TestPolicyPublishMissingPlaceholderWritesNothingWithRealDex(t *testing.T) {
	provider := newPolicyProvider(t)
	harness := newPublishHarness(t, provider)
	input := publishInput()
	input.Placeholders = append(input.Placeholders, docs.PlaceholderReplacement{Placeholder: "{{reviewCycle}}", Text: "annual"})

	outcome := harness.runPublish(t, "missing-placeholder", input)
	require.Equal(t, StatusPlaceholderMissing, outcome.Status)
	require.Equal(t, []string{"{{reviewCycle}}"}, outcome.MissingPlaceholders)
	require.NotNil(t, outcome.Document)
	require.Zero(t, provider.Count(fakegoogledocs.CountBatchUpdatesReceived), "no placeholder was filled")
}

func TestPolicyPublishInvalidRequestFailsBeforeCallingGoogleWithRealDex(t *testing.T) {
	provider := newPolicyProvider(t)
	harness := newPublishHarness(t, provider)

	flowID := harness.startPublish(t, "invalid", Input{Title: " ", Placeholders: integrationPlaceholders})
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.Count(fakegoogledocs.CountDocumentReads))
}

func publishInput() Input {
	return Input{Title: "Acme Refund Policy 2026", Placeholders: integrationPlaceholders}
}

func newPolicyProvider(t *testing.T) *fakegoogledocs.Server {
	t.Helper()
	provider := fakegoogledocs.New(t, integrationAccessToken)
	provider.AddDocument(fakegoogledocs.Document{ID: templateDocumentID, Title: "Refund Policy Template", Paragraphs: []fakegoogledocs.Paragraph{
		{Text: "Refund Policy", NamedStyleType: "HEADING_1"},
		{Text: "Effective {{effectiveDate}} for {{companyName}} customers."},
		{Text: "Approvals", NamedStyleType: "HEADING_2"},
		{Text: "Refunds above {{approvalThreshold}} need a manager.", IsBullet: true},
		{Text: "Refunds are paid within 5 business days.", IsBullet: true},
		{Text: "Questions go to {{policyOwner}}."},
	}})
	return provider
}

// requirePublishedDocument proves the document holds every value once and exactly one stamp.
func requirePublishedDocument(t *testing.T, provider *fakegoogledocs.Server, outcome Outcome) {
	t.Helper()
	require.NotNil(t, outcome.Document)
	published, isFound := provider.Document(outcome.Document.DocumentID)
	require.True(t, isFound)
	require.Len(t, published.Paragraphs, len(publishedPolicyParagraphs)+1)
	require.Equal(t, publishedPolicyParagraphs, published.Paragraphs[:len(publishedPolicyParagraphs)])
	stamp := published.Paragraphs[len(publishedPolicyParagraphs)]
	require.True(t, strings.HasPrefix(stamp.Text, "Published by Dex on "), stamp.Text)
	require.True(t, strings.HasSuffix(stamp.Text, " from template revision rev-policyTemplate1-1."), stamp.Text)
	require.Equal(t, 1, strings.Count(published.BodyText(), "Published by Dex"))
	template, _ := provider.Document(templateDocumentID)
	require.Contains(t, template.BodyText(), "{{effectiveDate}}", "the template is never written")
}

// publishHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type publishHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newPublishHarness(t *testing.T, provider *fakegoogledocs.Server) *publishHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}
	providerClient, err := docs.New(docs.Config{DocsEndpoint: provider.URL, DriveEndpoint: provider.URL},
		sdkgo.StaticCredentialProvider[docs.Credentials]{reference: {AccessToken: sdkgo.NewSecretString(integrationAccessToken)}},
		docs.WithHTTPClient(&http.Client{Timeout: providerRequestTimeout}),
	)
	require.NoError(t, err)
	connection, err := docs.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness := &publishHarness{
		flow: NewFlow(connection,
			sdkgo.ConnectorLoadedConfiguration[TemplateConfiguration]{Reference: TemplateConfigurationRef(), Value: TemplateConfiguration{DocumentID: templateDocumentID, DocumentTitle: "Refund Policy Template"}},
			sdkgo.ConnectorLoadedConfiguration[FolderConfiguration]{Reference: DestinationFolderConfigurationRef(), Value: FolderConfiguration{FolderID: destinationFolderID, FolderName: "Policies"}},
		),
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
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

func (harness *publishHarness) startWorker(t *testing.T) {
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
func (harness *publishHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *publishHarness) runPublish(t *testing.T, scenario string, input Input) Outcome {
	t.Helper()
	flowID := harness.startPublish(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func (harness *publishHarness) startPublish(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "policy-publish-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func (harness *publishHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	return result
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
