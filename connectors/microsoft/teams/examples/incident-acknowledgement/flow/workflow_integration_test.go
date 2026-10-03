//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package incidentacknowledgement

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationTeamID      = "fbe2bf47-16c8-47cf-b4a5-4b9b187c508b"
	integrationChannelID   = "19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2"
	integrationChatID      = "19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2"
	integrationAccessToken = "integration-access-token"
	integrationPosterID    = "8ea0e38b-efb3-4757-924a-5f94061cf8c2"
	integrationHumanID     = "5f1c2d3e-4b5a-4c6d-8e7f-901a2b3c4d5e"

	shortRequestTimeout = 500 * time.Millisecond
	// slowRequestTimeout outlasts the fake's nine-second responses and Dex's roughly seven-second async local phase.
	slowRequestTimeout = 20 * time.Second
	slowProviderDelay  = 9 * time.Second
	// workerLossInFlight lets the doomed Worker's post reach the fake before the process is killed.
	workerLossInFlight = 300 * time.Millisecond
)

// rootPostBehavior selects how the fake answers a root channel post.
type rootPostBehavior int

const (
	rootPostAnswered rootPostBehavior = iota
	// rootPostStoredWithoutAnswer stores the message, then never answers before the connector gives up.
	rootPostStoredWithoutAnswer
	// rootPostLostWithoutStoring never answers and never stores the message.
	rootPostLostWithoutStoring
	rootPostForbidden
)

var (
	channelMessagesPathPattern = regexp.MustCompile(`^/v1\.0/teams/([^/]+)/channels/([^/]+)/messages$`)
	threadRepliesPathPattern   = regexp.MustCompile(`^/v1\.0/teams/([^/]+)/channels/([^/]+)/messages/([0-9]+)/replies$`)
	chatMessagesPathPattern    = regexp.MustCompile(`^/v1\.0/chats/([^/]+)/messages$`)
)

func TestIncidentIsPostedRepliedAndAcknowledgedWithRealDex(t *testing.T) {
	provider := newFakeTeams(t)
	harness := newIncidentHarness(t, provider, shortRequestTimeout, EscalationChatSelection{})
	ctx := integrationContext(t)

	flowID := harness.startIncident(t, ctx, "acknowledged", incidentInput())
	record := harness.waitForRecord(t, ctx, flowID, func(record IncidentAcknowledgement) bool { return record.Phase == PhaseAwaitingAcknowledgement })
	provider.addReply(t, record.RootMessageID, integrationHumanID, "Ada Lovelace", "<p>Looking at the back end now</p>")
	provider.addReply(t, record.RootMessageID, integrationPosterID, "Incident Bot", "<p>ack</p>")
	harness.waitForRecord(t, ctx, flowID, func(record IncidentAcknowledgement) bool { return record.ReplyChecks >= 2 })
	acknowledgementID := provider.addReply(t, record.RootMessageID, integrationHumanID, "Ada Lovelace", `<p>ACK <emoji id="like" alt="👍" title="Like"></emoji></p>`)

	record = harness.waitForFlowOutput(t, ctx, flowID)
	require.Equal(t, PhaseAcknowledged, record.Phase)
	require.Equal(t, &Acknowledgement{
		MessageID: acknowledgementID, UserID: integrationHumanID, DisplayName: "Ada Lovelace", RepliedAt: provider.replyCreatedAt(t, record.RootMessageID, acknowledgementID),
	}, record.Acknowledgement, "neither the non-acknowledging reply nor the poster's own ack counts")
	require.Equal(t, integrationPosterID, record.PosterUserID)
	require.NotEmpty(t, record.StatusReplyID)
	require.False(t, record.IsRootConfirmedByReadBack)
	require.Equal(t, 1, provider.rootPostCount())
	require.Equal(t, 1, provider.replyPostCount())
	require.Zero(t, provider.chatPostCount())
	root := provider.root(t, record.RootMessageID)
	require.Equal(t, "INC-1042 [SEV2]: Checkout latency above SLO", root.subject)
	require.Equal(t, "<p>p95 checkout latency is 4.2 s &amp; rising<br>Owner: payments on-call</p>", root.content)
	require.Equal(t, "Status: SEV2 incident INC-1042 is being investigated. Reply ack in this thread to acknowledge.", provider.reply(t, record.RootMessageID, record.StatusReplyID).content)
}

// TestSlowPostsAreSentOnceUnderSyncDurabilityWithRealDex guards sync durability: an async backup attempt
// would send a post that outlasts Dex's roughly seven-second local phase while the first is still in flight.
func TestSlowPostsAreSentOnceUnderSyncDurabilityWithRealDex(t *testing.T) {
	provider := newFakeTeams(t)
	provider.setPostDelay(slowProviderDelay)
	harness := newIncidentHarness(t, provider, slowRequestTimeout, EscalationChatSelection{})
	ctx := integrationContext(t)

	started := time.Now()
	flowID := harness.startIncident(t, ctx, "slow-posts", incidentInput())
	record := harness.waitForRecord(t, ctx, flowID, func(record IncidentAcknowledgement) bool { return record.Phase == PhaseAwaitingAcknowledgement })
	require.GreaterOrEqual(t, time.Since(started), 2*slowProviderDelay, "both posts waited for the slow fake")
	provider.addReply(t, record.RootMessageID, integrationHumanID, "Ada Lovelace", "<p>ack</p>")

	record = harness.waitForFlowOutput(t, ctx, flowID)
	require.Equal(t, PhaseAcknowledged, record.Phase)
	require.False(t, record.IsRootConfirmedByReadBack, "the slow attempt itself reported the post")
	require.Equal(t, 1, provider.rootPostCount(), "one Step execution sends one channel message")
	require.Equal(t, 1, provider.replyPostCount(), "one Step execution sends one thread reply")
	require.Equal(t, 1, provider.storedRootCount())
}

func TestUnansweredPostIsConfirmedByReadBackWithoutAResendWithRealDex(t *testing.T) {
	provider := newFakeTeams(t)
	provider.setRootPostBehavior(rootPostStoredWithoutAnswer)
	harness := newIncidentHarness(t, provider, shortRequestTimeout, EscalationChatSelection{})
	ctx := integrationContext(t)

	flowID := harness.startIncident(t, ctx, "read-back", incidentInput())
	record := harness.waitForRecord(t, ctx, flowID, func(record IncidentAcknowledgement) bool { return record.Phase == PhaseAwaitingAcknowledgement })
	require.True(t, record.IsRootConfirmedByReadBack)
	require.Equal(t, provider.onlyRootID(t), record.RootMessageID)
	require.Equal(t, integrationPosterID, record.PosterUserID)
	provider.addReply(t, record.RootMessageID, integrationHumanID, "Ada Lovelace", "<p>ack</p>")
	require.Equal(t, PhaseAcknowledged, harness.waitForFlowOutput(t, ctx, flowID).Phase)
	require.Equal(t, 1, provider.rootPostCount(), "an unconfirmed post is never sent again")
}

func TestLostPostWithoutAMatchIsNeverResentWithRealDex(t *testing.T) {
	provider := newFakeTeams(t)
	provider.setRootPostBehavior(rootPostLostWithoutStoring)
	harness := newIncidentHarness(t, provider, shortRequestTimeout, EscalationChatSelection{})
	ctx := integrationContext(t)

	record := harness.waitForFlowOutput(t, ctx, harness.startIncident(t, ctx, "uncertain", incidentInput()))
	require.Equal(t, PhasePostOutcomeUnknown, record.Phase)
	require.Equal(t, sdkgo.FailureTransport, record.FailureKind)
	require.Equal(t, 1, provider.rootPostCount())
	require.Zero(t, provider.replyPostCount())
}

func TestWorkerLostDuringPostIsReconciledWithoutPostingAgainWithRealDex(t *testing.T) {
	provider := newFakeTeams(t)
	provider.setRootPostBehavior(rootPostStoredWithoutAnswer)
	ctx := integrationContext(t)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	process := startWorkerProcess(t, provider, workerAddress)

	harness := newIncidentHarnessWithWorkerAddress(t, provider, shortRequestTimeout, EscalationChatSelection{}, workerAddress, false)
	flowID := harness.startIncident(t, ctx, "worker-lost", incidentInput())
	select {
	case <-provider.rootPostReceived:
	case <-ctx.Done():
		t.Fatal("the Worker process never sent the channel post")
	}
	time.Sleep(workerLossInFlight)
	require.NoError(t, process.Process.Signal(syscall.SIGKILL))
	_ = process.Wait() // The killed process always exits with a signal error.
	provider.setRootPostBehavior(rootPostAnswered)
	harness.startWorker(t)

	record := harness.waitForRecord(t, ctx, flowID, func(record IncidentAcknowledgement) bool { return record.Phase == PhaseAwaitingAcknowledgement })
	require.True(t, record.IsRootConfirmedByReadBack, "the replacement attempt found the checkpoint and read the channel back")
	require.Equal(t, provider.onlyRootID(t), record.RootMessageID)
	require.Equal(t, 1, provider.rootPostCount(), "the replacement attempt did not post again")
}

func TestUnacknowledgedIncidentEscalatesOnceToTheChatWithRealDex(t *testing.T) {
	provider := newFakeTeams(t)
	provider.setChatPostDelay(slowProviderDelay)
	harness := newIncidentHarness(t, provider, slowRequestTimeout, EscalationChatSelection{ChatID: integrationChatID, ChatName: "On-call managers"})
	ctx := integrationContext(t)

	record := harness.waitForFlowOutput(t, ctx, harness.startIncident(t, ctx, "escalated", incidentInput()))
	require.Equal(t, PhaseEscalated, record.Phase)
	require.Equal(t, 3, record.ReplyChecks)
	require.Nil(t, record.Acknowledgement)
	require.NotEmpty(t, record.EscalationMessageID)
	require.Equal(t, 1, provider.chatPostCount(), "the nine-second chat message was sent once")
	require.Contains(t, provider.chatMessage(t, 0), "Nobody has acknowledged SEV2 incident INC-1042")
}

func TestRepliesWithoutAdministratorConsentCompleteAsUnreadableWithRealDex(t *testing.T) {
	provider := newFakeTeams(t)
	provider.setRepliesForbidden()
	harness := newIncidentHarness(t, provider, shortRequestTimeout, EscalationChatSelection{})
	ctx := integrationContext(t)

	record := harness.waitForFlowOutput(t, ctx, harness.startIncident(t, ctx, "no-consent", incidentInput()))
	require.Equal(t, PhaseRepliesUnreadable, record.Phase)
	require.Equal(t, sdkgo.FailureAuthorization, record.FailureKind)
	require.Equal(t, 1, provider.rootPostCount())
}

func TestRejectedChannelPostCompletesAsRejectedWithRealDex(t *testing.T) {
	provider := newFakeTeams(t)
	provider.setRootPostBehavior(rootPostForbidden)
	harness := newIncidentHarness(t, provider, shortRequestTimeout, EscalationChatSelection{})
	ctx := integrationContext(t)

	record := harness.waitForFlowOutput(t, ctx, harness.startIncident(t, ctx, "rejected", incidentInput()))
	require.Equal(t, PhaseRejected, record.Phase)
	require.Equal(t, sdkgo.FailureAuthorization, record.FailureKind)
	require.Empty(t, record.RootMessageID)
	require.Zero(t, provider.replyPostCount())
}

func incidentInput() Input {
	return Input{
		IncidentID: "INC-1042", Title: "Checkout latency above SLO", Severity: "SEV2",
		Summary: "p95 checkout latency is 4.2 s & rising\nOwner: payments on-call",
	}
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// incidentHarness runs the example Flow on a real Dex Server with an in-process or replaceable Worker.
type incidentHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	client        *dex.Client
	worker        *dex.Worker
	workerResult  chan error
	workerAddress string
	serverAddress string
}

func newIncidentHarness(t *testing.T, provider *fakeTeams, requestTimeout time.Duration, escalation EscalationChatSelection) *incidentHarness {
	t.Helper()
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	return newIncidentHarnessWithWorkerAddress(t, provider, requestTimeout, escalation, workerAddress, true)
}

func newIncidentHarnessWithWorkerAddress(
	t *testing.T,
	provider *fakeTeams,
	requestTimeout time.Duration,
	escalation EscalationChatSelection,
	workerAddress string,
	isWorkerStarted bool,
) *incidentHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "microsoft", Name: ConnectionName}
	providerClient, err := teams.New(
		teams.Config{Endpoint: provider.URL + "/v1.0"},
		sdkgo.StaticCredentialProvider[teams.Credentials]{reference: {AccessToken: sdkgo.NewSecretString(integrationAccessToken)}},
		teams.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := teams.NewConnection(providerClient, reference)
	require.NoError(t, err)
	policy := AcknowledgementPolicy{CheckInterval: time.Second, MaximumChecks: 3}
	flow := NewFlow(connection, ChannelSelection{TeamID: integrationTeamID, ChannelID: integrationChannelID}, escalation, &policy)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness := &incidentHarness{
		flow: flow, registry: registry, cache: cache, workerAddress: workerAddress,
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		var stopErr error
		if harness.worker != nil {
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stopErr = errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult)
		}
		require.NoError(t, errors.Join(stopErr, harness.client.Close(), harness.cache.Close()))
	})
	if isWorkerStarted {
		harness.startWorker(t)
	}
	return harness
}

func (harness *incidentHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
}

func (harness *incidentHarness) startIncident(t *testing.T, ctx context.Context, scenario string, input Input) string {
	t.Helper()
	flowID := "teams-incident-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := harness.client.StopFlow(stopCtx, flowID, dex.StopOptions{Type: dex.CancelFlow, Reason: "integration test finished"})
		// Dex Go SDK releases name the closed-Flow error type differently; each embeds this sub-status.
		var serviceError *dex.ServiceError
		isClosedFlow := errors.As(err, &serviceError) && serviceError.SubStatus == dex.ErrorSubStatusFlowNotFound
		if !isClosedFlow {
			require.NoError(t, err, "a Flow still waiting for an acknowledgement is cancelled")
		}
	})
	return flowID
}

func (harness *incidentHarness) waitForFlowOutput(t *testing.T, ctx context.Context, flowID string) IncidentAcknowledgement {
	t.Helper()
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s", flowID)
	var record IncidentAcknowledgement
	require.NoError(t, result.DecodeSingleOutput(&record))
	encoded, err := json.Marshal(record)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL", "no provider error text reaches Flow state")
	require.NotContains(t, string(encoded), integrationAccessToken)
	return record
}

// waitForRecord polls until the record is expected; a record that reached a terminal phase first fails at once.
func (harness *incidentHarness) waitForRecord(t *testing.T, ctx context.Context, flowID string, isExpected func(IncidentAcknowledgement) bool) IncidentAcknowledgement {
	t.Helper()
	var record IncidentAcknowledgement
	require.Eventually(t, func() bool {
		record = IncidentAcknowledgement{}
		err := harness.client.InvokeRPC(ctx, flowID, harness.flow.GetIncidentAcknowledgement, nil, &record)
		return err == nil && (isExpected(record) || isTerminalPhase(record.Phase))
	}, 2*time.Minute, 100*time.Millisecond, "Flow %s last record %+v", flowID, &record)
	require.True(t, isExpected(record), "Flow %s stopped at %+v", flowID, record)
	return record
}

func isTerminalPhase(phase string) bool {
	switch phase {
	case PhaseAcknowledged, PhaseEscalated, PhaseUnacknowledged, PhaseRejected, PhasePostOutcomeUnknown, PhaseRepliesUnreadable:
		return true
	default:
		return false
	}
}

// fakeTeams is a credential-safe Microsoft Graph Teams fake: it stores channel messages, replies, and chat
// messages, posts as integrationPosterID, and lists messages newest first as Graph does in its examples.
type fakeTeams struct {
	*httptest.Server
	t                *testing.T
	mutex            sync.Mutex
	nextID           int64
	roots            []*fakeMessage
	replies          map[string][]*fakeMessage
	chatMessages     []string
	rootPosts        int
	replyPosts       int
	chatPosts        int
	postDelay        time.Duration
	chatPostDelay    time.Duration
	rootBehavior     rootPostBehavior
	isRepliesBlocked bool
	rootPostReceived chan struct{}
	rootPostSignal   sync.Once
}

type fakeMessage struct {
	id          string
	replyToID   string
	subject     string
	contentType string
	content     string
	userID      string
	displayName string
	createdAt   time.Time
}

func newFakeTeams(t *testing.T) *fakeTeams {
	t.Helper()
	provider := &fakeTeams{t: t, nextID: time.Now().UnixMilli(), replies: map[string][]*fakeMessage{}, rootPostReceived: make(chan struct{})}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeTeams) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+integrationAccessToken {
		provider.writeJSON(response, http.StatusUnauthorized, `{"error":{"code":"InvalidAuthenticationToken","message":"SENTINEL"}}`)
		return
	}
	switch {
	case channelMessagesPathPattern.MatchString(request.URL.Path) && request.Method == http.MethodPost:
		provider.postRoot(response, request)
	case channelMessagesPathPattern.MatchString(request.URL.Path) && request.Method == http.MethodGet:
		provider.listRoots(response)
	case threadRepliesPathPattern.MatchString(request.URL.Path):
		rootID := threadRepliesPathPattern.FindStringSubmatch(request.URL.Path)[3]
		if request.Method == http.MethodPost {
			provider.postReply(response, request, rootID)
			return
		}
		provider.listReplies(response, rootID)
	case chatMessagesPathPattern.MatchString(request.URL.Path) && request.Method == http.MethodPost:
		provider.postChatMessage(response, request)
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"error":{"code":"NotFound","message":"SENTINEL"}}`)
	}
}

func (provider *fakeTeams) postRoot(response http.ResponseWriter, request *http.Request) {
	body := provider.decodeBody(request)
	provider.mutex.Lock()
	provider.rootPosts++
	behavior, delay := provider.rootBehavior, provider.postDelay
	var message *fakeMessage
	if behavior != rootPostForbidden && behavior != rootPostLostWithoutStoring {
		message = provider.storeLocked("", body)
		provider.roots = append(provider.roots, message)
	}
	provider.mutex.Unlock()
	provider.rootPostSignal.Do(func() { close(provider.rootPostReceived) })
	switch behavior {
	case rootPostForbidden:
		provider.writeJSON(response, http.StatusForbidden, `{"error":{"code":"Forbidden","message":"SENTINEL not a member"}}`)
	case rootPostStoredWithoutAnswer, rootPostLostWithoutStoring:
		<-request.Context().Done()
	default:
		time.Sleep(delay)
		provider.writeJSON(response, http.StatusCreated, provider.messageJSON(message))
	}
}

func (provider *fakeTeams) postReply(response http.ResponseWriter, request *http.Request, rootID string) {
	body := provider.decodeBody(request)
	provider.mutex.Lock()
	provider.replyPosts++
	message := provider.storeLocked(rootID, body)
	provider.replies[rootID] = append(provider.replies[rootID], message)
	delay := provider.postDelay
	provider.mutex.Unlock()
	time.Sleep(delay)
	provider.writeJSON(response, http.StatusCreated, provider.messageJSON(message))
}

func (provider *fakeTeams) postChatMessage(response http.ResponseWriter, request *http.Request) {
	body := provider.decodeBody(request)
	provider.mutex.Lock()
	provider.chatPosts++
	provider.chatMessages = append(provider.chatMessages, body.Body.Content)
	message := provider.storeLocked("", body)
	delay := provider.chatPostDelay
	provider.mutex.Unlock()
	time.Sleep(delay)
	provider.writeJSON(response, http.StatusCreated, provider.messageJSON(message))
}

func (provider *fakeTeams) listRoots(response http.ResponseWriter) {
	provider.mutex.Lock()
	messages := append([]*fakeMessage(nil), provider.roots...)
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, provider.collectionJSON(messages))
}

func (provider *fakeTeams) listReplies(response http.ResponseWriter, rootID string) {
	provider.mutex.Lock()
	isBlocked := provider.isRepliesBlocked
	messages := append([]*fakeMessage(nil), provider.replies[rootID]...)
	provider.mutex.Unlock()
	if isBlocked {
		provider.writeJSON(response, http.StatusForbidden, `{"error":{"code":"Forbidden","message":"SENTINEL Missing role permissions on the request."}}`)
		return
	}
	provider.writeJSON(response, http.StatusOK, provider.collectionJSON(messages))
}

type fakeCreateBody struct {
	Subject string `json:"subject"`
	Body    struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
}

func (provider *fakeTeams) decodeBody(request *http.Request) fakeCreateBody {
	contents, err := io.ReadAll(request.Body)
	require.NoError(provider.t, err)
	var body fakeCreateBody
	require.NoError(provider.t, json.Unmarshal(contents, &body))
	return body
}

func (provider *fakeTeams) storeLocked(replyToID string, body fakeCreateBody) *fakeMessage {
	provider.nextID++
	return &fakeMessage{
		id: strconv.FormatInt(provider.nextID, 10), replyToID: replyToID, subject: body.Subject,
		contentType: body.Body.ContentType, content: body.Body.Content, userID: integrationPosterID, displayName: "Incident Bot",
		createdAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

// addReply stores a person's reply in the thread, as someone typing in Teams would, and returns its ID.
func (provider *fakeTeams) addReply(t *testing.T, rootID string, userID string, displayName string, content string) string {
	t.Helper()
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextID++
	message := &fakeMessage{
		id: strconv.FormatInt(provider.nextID, 10), replyToID: rootID, contentType: "html", content: content,
		userID: userID, displayName: displayName, createdAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	provider.replies[rootID] = append(provider.replies[rootID], message)
	return message.id
}

func (provider *fakeTeams) messageJSON(message *fakeMessage) string {
	encoded, err := json.Marshal(provider.messageResource(message))
	require.NoError(provider.t, err)
	return string(encoded)
}

func (provider *fakeTeams) collectionJSON(messages []*fakeMessage) string {
	value := make([]map[string]any, 0, len(messages))
	for index := len(messages) - 1; index >= 0; index-- {
		value = append(value, provider.messageResource(messages[index]))
	}
	encoded, err := json.Marshal(map[string]any{"@odata.context": "https://graph.microsoft.com/v1.0/$metadata#chatMessages", "value": value})
	require.NoError(provider.t, err)
	return string(encoded)
}

func (provider *fakeTeams) messageResource(message *fakeMessage) map[string]any {
	created := message.createdAt.Format("2006-01-02T15:04:05.000Z")
	resource := map[string]any{
		"id": message.id, "etag": message.id, "messageType": "message", "createdDateTime": created, "lastModifiedDateTime": created,
		"lastEditedDateTime": nil, "deletedDateTime": nil, "subject": nil, "importance": "normal", "replyToId": nil,
		"webUrl": "https://teams.microsoft.com/l/message/" + integrationChannelID + "/" + message.id,
		"from": map[string]any{"application": nil, "device": nil, "user": map[string]any{
			"id": message.userID, "displayName": message.displayName, "userIdentityType": "aadUser",
		}},
		"body":            map[string]any{"contentType": message.contentType, "content": message.content},
		"channelIdentity": map[string]any{"teamId": integrationTeamID, "channelId": integrationChannelID},
	}
	if message.subject != "" {
		resource["subject"] = message.subject
	}
	if message.replyToID != "" {
		resource["replyToId"] = message.replyToID
	}
	return resource
}

func (provider *fakeTeams) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("request-id", "6e1f0a3b-2c4d-4e5f-8a9b-0c1d2e3f4a5b")
	response.WriteHeader(status)
	// The connector may have given up on a slow response; a failed write is the scenario, not a fake defect.
	_, _ = response.Write([]byte(body))
}

func (provider *fakeTeams) setPostDelay(delay time.Duration) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.postDelay = delay
}

func (provider *fakeTeams) setChatPostDelay(delay time.Duration) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.chatPostDelay = delay
}

func (provider *fakeTeams) setRootPostBehavior(behavior rootPostBehavior) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.rootBehavior = behavior
}

func (provider *fakeTeams) setRepliesForbidden() {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.isRepliesBlocked = true
}

func (provider *fakeTeams) rootPostCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.rootPosts
}

func (provider *fakeTeams) storedRootCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.roots)
}

func (provider *fakeTeams) replyPostCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.replyPosts
}

func (provider *fakeTeams) chatPostCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.chatPosts
}

func (provider *fakeTeams) chatMessage(t *testing.T, index int) string {
	t.Helper()
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Greater(t, len(provider.chatMessages), index)
	return provider.chatMessages[index]
}

func (provider *fakeTeams) onlyRootID(t *testing.T) string {
	t.Helper()
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Len(t, provider.roots, 1)
	return provider.roots[0].id
}

func (provider *fakeTeams) root(t *testing.T, rootID string) fakeMessage {
	t.Helper()
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for _, message := range provider.roots {
		if message.id == rootID {
			return *message
		}
	}
	t.Fatalf("root message %s was not posted", rootID)
	return fakeMessage{}
}

func (provider *fakeTeams) reply(t *testing.T, rootID string, replyID string) fakeMessage {
	t.Helper()
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for _, message := range provider.replies[rootID] {
		if message.id == replyID {
			return *message
		}
	}
	t.Fatalf("reply %s was not posted", replyID)
	return fakeMessage{}
}

func (provider *fakeTeams) replyCreatedAt(t *testing.T, rootID string, replyID string) time.Time {
	t.Helper()
	return provider.reply(t, rootID, replyID).createdAt
}

// startWorkerProcess runs the example's own Worker binary against the fake, so the test can kill it mid-request.
func startWorkerProcess(t *testing.T, provider *fakeTeams, workerAddress string) *exec.Cmd {
	t.Helper()
	directory := t.TempDir()
	binary := filepath.Join(directory, "incident-acknowledgement-worker")
	build := exec.Command("go", "build", "-o", binary, "..")
	build.Env = append(os.Environ(), "GOWORK=off")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))

	connections, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []map[string]any{{
			"connectorId": teams.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/microsoft/teams",
			"moduleVersion": "v0.1.0", "provider": "microsoft", "connectionName": ConnectionName,
			"configuration": map[string]any{"endpoint": provider.URL + "/v1.0"},
			"credentials": map[string]any{
				"oauth_client_id": "client-id", "oauth_client_secret": "client-secret",
				"access_token": integrationAccessToken, "refresh_token": "refresh-token",
			},
			"credentialExpiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "connections.json"), connections, 0o600))
	useConfigurations, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.UseConfigurationsSchemaVersion,
		"operationConfigurations": []map[string]any{{
			"connectorId": teams.ConnectorID, "connectionName": ConnectionName, "operationId": "postChannelMessage",
			"flowType": FlowType, "stepType": postIncidentUpdateStepType,
			"configuration": map[string]any{"teamId": integrationTeamID, "channelId": integrationChannelID},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, localconfig.UseConfigurationsFileName), useConfigurations, 0o600))

	process := exec.Command(binary)
	process.Env = append(os.Environ(),
		"DEX_CONNECTOR_CONFIG_FILE="+filepath.Join(directory, "connections.json"), "DEX_WORKER_BIND_ADDRESS="+workerAddress,
		"DEX_FLOW_SERVICE_ADDRESS="+environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
		"DEX_BLOB_CACHE_DIR="+filepath.Join(directory, "blobs"),
	)
	logFile, err := os.Create(filepath.Join(directory, "worker.log"))
	require.NoError(t, err)
	process.Stdout, process.Stderr = logFile, logFile
	require.NoError(t, process.Start())
	t.Cleanup(func() {
		// Kill is a no-op error after the test already killed the process.
		_ = process.Process.Kill()
		require.NoError(t, logFile.Close())
	})
	require.Eventually(t, func() bool {
		connection, err := net.DialTimeout("tcp", workerAddress, 100*time.Millisecond)
		if err != nil {
			return false
		}
		return connection.Close() == nil
	}, time.Minute, 100*time.Millisecond, "the Worker process listens on %s", workerAddress)
	return process
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
