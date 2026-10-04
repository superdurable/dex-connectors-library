// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
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
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/helpscout/examples/conversation-triage/flow"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// sentinelToken stands in for the OAuth access token; no log record or response may contain it.
	sentinelToken = "SENTINEL-HELPSCOUT-ACCESS-TOKEN"
	// sentinelWebhookSecret stands in for the webhook secret.
	sentinelWebhookSecret = "SENTINEL-HELPSCOUT-WEBHOOK-SECRET"

	triagedInboxID  int64 = 123
	otherInboxID    int64 = 456
	customerEmail         = "jane@acme.example.com"
	otherActiveConv int64 = 502
)

// mailboxBinding is the mailboxPicker binding Dex Web saves: only the triaged inbox starts a Flow.
var mailboxBinding = helpscout.ConversationEventTriggerConfiguration{MailboxID: triagedInboxID}

// newExampleCredentials holds the credentials Dex Web saves, with no access token yet. The refreshing source
// replaces project storage and keeps the token the connector obtains.
func newExampleCredentials() *testsupport.RefreshingCredentialSource[helpscout.Credentials] {
	return testsupport.NewRefreshingCredentialSource(helpscout.Credentials{
		AppID: "app-id", AppSecret: sdkgo.NewSecretString("app-secret"), WebhookSecret: sdkgo.NewSecretString(sentinelWebhookSecret),
	}, nil)
}

// newExampleConnection builds the example's connection on credentials.
func newExampleConnection(t *testing.T, credentials helpscout.CredentialSource, options ...helpscout.Option) helpscout.Connection {
	t.Helper()
	client, err := helpscout.New(helpscout.Config{}, credentials, options...)
	require.NoError(t, err)
	connection, err := helpscout.NewConnection(client, sdkgo.ConnectionRef{Provider: "helpscout", Name: conversationtriage.ConnectionName})
	require.NoError(t, err)
	return connection
}

// conversationEndpoint serves the example's target like newConversationEndpointRunner, without the durable
// project inbox.
type conversationEndpoint struct {
	server         *httptest.Server
	endpointRunner *webhooktrigger.EndpointRunner
	readiness      interface{ RunningSourceCount() int }
}

func newConversationEndpoint(
	t *testing.T, connection helpscout.Connection, target sdkgo.TriggerTarget[helpscout.ConversationEvent],
) *conversationEndpoint {
	t.Helper()
	handler, err := connection.ConversationEventWebhookHandler()
	require.NoError(t, err)
	trigger := helpscout.NewConversationEventTrigger(helpscout.ConversationEventTriggerConfig{
		Connection: connection, ConnectionName: conversationtriage.ConnectionName,
		BindingName: conversationtriage.NewConversationTriggerBinding, Configuration: mailboxBinding, Target: target,
	})
	endpointRunner, err := webhooktrigger.NewEndpointRunner(handler, trigger)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(webhookPath, endpointRunner)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &conversationEndpoint{server: server, endpointRunner: endpointRunner, readiness: handler.(interface{ RunningSourceCount() int })}
}

// start runs the binding until the test ends and waits until it receives deliveries.
func (endpoint *conversationEndpoint) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- endpoint.endpointRunner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	})
	require.Eventually(t, func() bool { return endpoint.readiness.RunningSourceCount() == 1 }, 10*time.Second, 10*time.Millisecond,
		"the Help Scout binding must start receiving")
}

// deliverWebhook posts body as Help Scout does, signed with secret; tamper changes it after signing.
func (endpoint *conversationEndpoint) deliverWebhook(t *testing.T, event string, body string, secret string, isTampered bool) int {
	t.Helper()
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(body))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if isTampered {
		body = strings.Replace(body, "jane@", "eve@", 1)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint.server.URL+webhookPath, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-HelpScout-Event", event)
	request.Header.Set("X-HelpScout-Signature", signature)
	response, err := endpoint.server.Client().Do(request)
	require.NoError(t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.NotContains(t, string(responseBody), sentinelWebhookSecret)
	return response.StatusCode
}

// conversationWebhookBody is a v2 Conversation object as a Help Scout conversation webhook carries it.
func conversationWebhookBody(conversationID int64, mailboxID int64, status string) string {
	return fmt.Sprintf(`{"id":%d,"number":%d,"threads":1,"type":"email","folderId":11,"status":%q,"state":"published",`+
		`"subject":"Double charge on order 88213","preview":"I was charged twice","mailboxId":%d,`+
		`"createdBy":{"id":238604,"type":"customer","email":%q},"createdAt":"2026-09-30T11:58:00Z",`+
		`"primaryCustomer":{"id":238604,"type":"customer","first":"Jane","last":"Smith","email":%q},`+
		`"source":{"type":"email","via":"customer"},"tags":[{"id":9150,"tag":"billing"}],`+
		`"_embedded":{"threads":[{"id":%d,"type":"customer","body":"I was charged twice.","createdAt":"2026-09-30T11:58:00Z"}]}}`,
		conversationID, conversationID%1000, status, mailboxID, customerEmail, customerEmail, conversationID*10)
}

// fakeHelpScout answers every request the triage Flow sends, keeping each conversation's tags and notes.
type fakeHelpScout struct {
	*httptest.Server
	mu       sync.Mutex
	tags     map[int64][]string
	notes    map[int64][]string
	requests map[string]int
}

func newFakeHelpScout(t *testing.T) *fakeHelpScout {
	t.Helper()
	fake := &fakeHelpScout{tags: map[int64][]string{}, notes: map[int64][]string{}, requests: map[string]int{}}
	fake.Server = httptest.NewTLSServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *fakeHelpScout) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodPost && request.URL.Path == "/v2/oauth2/token" {
		fake.issueAccessToken(response, request)
		return
	}
	if request.Header.Get("Authorization") != "Bearer "+sentinelToken {
		writeJSON(response, http.StatusUnauthorized, `{"message":"Unauthorized"}`)
		return
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.requests[request.Method+" "+request.URL.Path]++
	path, query := request.URL.Path, request.URL.Query()
	switch {
	case request.Method == http.MethodGet && path == "/v3/customers" && query.Get("email") == customerEmail:
		writeJSON(response, http.StatusOK, `{"_embedded":{"customers":[`+customerJSON(1001)+`,`+customerJSON(1002)+`]},"_links":{"self":{"href":"x"}}}`)
	case request.Method == http.MethodGet && path == "/v2/conversations":
		fake.searchConversations(response, query)
	case strings.HasPrefix(path, "/v2/conversations/"):
		fake.serveConversation(response, request)
	default:
		writeJSON(response, http.StatusNotFound, `{"message":"Not Found"}`)
	}
}

// issueAccessToken answers Help Scout's documented client credentials exchange for the fixture's app.
func (fake *fakeHelpScout) issueAccessToken(response http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil || request.PostForm.Get("grant_type") != "client_credentials" ||
		request.PostForm.Get("client_id") != "app-id" || request.PostForm.Get("client_secret") != "app-secret" {
		writeJSON(response, http.StatusUnauthorized, `{"error":"invalid_client"}`)
		return
	}
	fake.mu.Lock()
	fake.requests["POST /v2/oauth2/token"]++
	fake.mu.Unlock()
	writeJSON(response, http.StatusOK, `{"token_type":"bearer","access_token":"`+sentinelToken+`","expires_in":172800}`)
}

// searchConversations lists the customer's active conversations in the triaged inbox: the new one and 502.
func (fake *fakeHelpScout) searchConversations(response http.ResponseWriter, query url.Values) {
	if query.Get("status") != "active" || query.Get("query") != `(email:"`+customerEmail+`")` {
		writeJSON(response, http.StatusBadRequest, `{"message":"Bad request"}`)
		return
	}
	mailboxID, _ := strconv.ParseInt(query.Get("mailbox"), 10, 64) // An invalid mailbox lists nothing.
	conversations := []string{}
	if mailboxID == triagedInboxID {
		conversationIDs := slices.Sorted(maps.Keys(fake.tags))
		for _, conversationID := range conversationIDs {
			conversations = append(conversations, fake.conversationJSON(conversationID))
		}
		if !slices.Contains(conversationIDs, otherActiveConv) {
			conversations = append(conversations, conversationWebhookBody(otherActiveConv, triagedInboxID, "active"))
		}
	}
	writeJSON(response, http.StatusOK, fmt.Sprintf(`{"_embedded":{"conversations":[%s]},"_links":{"self":{"href":"x"}},"page":{"number":1,"size":25,"totalElements":%d,"totalPages":1}}`,
		strings.Join(conversations, ","), len(conversations)))
}

func (fake *fakeHelpScout) serveConversation(response http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/v2/conversations/"), "/")
	conversationID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || conversationID < 1 {
		writeJSON(response, http.StatusNotFound, `{"message":"Not Found"}`)
		return
	}
	if _, isKnown := fake.tags[conversationID]; !isKnown {
		fake.tags[conversationID] = []string{"billing"}
	}
	switch {
	case request.Method == http.MethodGet && len(parts) == 1:
		writeJSON(response, http.StatusOK, fake.conversationJSON(conversationID))
	case request.Method == http.MethodGet && len(parts) == 2 && parts[1] == "threads":
		writeJSON(response, http.StatusOK, fmt.Sprintf(`{"_embedded":{"threads":[{"id":%d,"type":"customer","status":"active","state":"published","body":"I was charged twice.","createdBy":{"id":238604,"type":"customer","email":%q},"createdAt":"2026-09-30T11:58:00Z"}]},"_links":{"self":{"href":"x"}}}`,
			conversationID*10, customerEmail))
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "notes":
		var note struct {
			Text string `json:"text"`
		}
		if json.NewDecoder(request.Body).Decode(&note) != nil || note.Text == "" {
			writeJSON(response, http.StatusBadRequest, `{"message":"Bad request"}`)
			return
		}
		fake.notes[conversationID] = append(fake.notes[conversationID], note.Text)
		response.Header().Set("Resource-Id", strconv.FormatInt(conversationID*10+int64(len(fake.notes[conversationID])), 10))
		response.WriteHeader(http.StatusCreated)
	case request.Method == http.MethodPut && len(parts) == 2 && parts[1] == "tags":
		var body struct {
			Tags []string `json:"tags"`
		}
		if json.NewDecoder(request.Body).Decode(&body) != nil || body.Tags == nil {
			writeJSON(response, http.StatusBadRequest, `{"message":"Bad request"}`)
			return
		}
		fake.tags[conversationID] = body.Tags
		response.WriteHeader(http.StatusNoContent)
	default:
		writeJSON(response, http.StatusNotFound, `{"message":"Not Found"}`)
	}
}

func (fake *fakeHelpScout) conversationJSON(conversationID int64) string {
	tagObjects := make([]string, 0, len(fake.tags[conversationID]))
	for _, tag := range fake.tags[conversationID] {
		tagObjects = append(tagObjects, fmt.Sprintf(`{"tag":%q}`, tag))
	}
	body := conversationWebhookBody(conversationID, triagedInboxID, "active")
	return strings.Replace(body, `"tags":[{"id":9150,"tag":"billing"}]`, `"tags":[`+strings.Join(tagObjects, ",")+`]`, 1)
}

func (fake *fakeHelpScout) notesFor(conversationID int64) []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]string(nil), fake.notes[conversationID]...)
}

func (fake *fakeHelpScout) tagsFor(conversationID int64) []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]string(nil), fake.tags[conversationID]...)
}

func (fake *fakeHelpScout) requestCount(method string, path string) int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.requests[method+" "+path]
}

// connectionOption sends the connector's api.helpscout.net requests to the fake server.
func (fake *fakeHelpScout) connectionOption() helpscout.Option {
	target, err := url.Parse(fake.URL)
	if err != nil {
		panic(err)
	}
	base := fake.Client().Transport
	return helpscout.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.URL.Scheme, redirected.URL.Host, redirected.Host = target.Scheme, target.Host, target.Host
		return base.RoundTrip(redirected)
	})})
}

func customerJSON(customerID int64) string {
	return fmt.Sprintf(`{"id":%d,"firstName":"Jane","lastName":"Smith","conversationCount":2,"createdAt":"2026-01-10T12:34:12Z","_embedded":{"emails":[{"id":1,"value":%q,"type":"work"}]}}`,
		customerID, customerEmail)
}

func writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/hal+json")
	response.WriteHeader(status)
	_, _ = io.WriteString(response, body) // A failed write fails the Flow under test instead.
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func unusedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

// recordedLogs keeps every record, at every level, as a message plus flattened attributes.
type recordedLogs struct {
	mu      sync.Mutex
	records []recordedLog
	output  strings.Builder
}

type recordedLog struct {
	message string
	attrs   map[string]string
}

func newRecordedLogs(t *testing.T) *recordedLogs {
	t.Helper()
	logs := &recordedLogs{}
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("captured logs:\n%s", logs.text())
		}
	})
	return logs
}

func (logs *recordedLogs) logger() *slog.Logger {
	return slog.New(recordedLogHandler{logs: logs})
}

// find returns the records with message whose attributes include every entry of attrs.
func (logs *recordedLogs) find(message string, attrs map[string]string) []recordedLog {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	matches := []recordedLog{}
	for _, record := range logs.records {
		if record.message != message {
			continue
		}
		isMatch := true
		for key, value := range attrs {
			isMatch = isMatch && record.attrs[key] == value
		}
		if isMatch {
			matches = append(matches, record)
		}
	}
	return matches
}

func (logs *recordedLogs) text() string {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	return logs.output.String()
}

type recordedLogHandler struct {
	logs  *recordedLogs
	attrs []slog.Attr
}

func (recordedLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (handler recordedLogHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := map[string]string{}
	for _, attr := range handler.attrs {
		attrs[attr.Key] = attr.Value.String()
	}
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.String()
		return true
	})
	handler.logs.mu.Lock()
	defer handler.logs.mu.Unlock()
	handler.logs.records = append(handler.logs.records, recordedLog{message: record.Message, attrs: attrs})
	fmt.Fprintf(&handler.logs.output, "level=%s msg=%q attrs=%v\n", record.Level, record.Message, attrs)
	return nil
}

func (handler recordedLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return recordedLogHandler{logs: handler.logs, attrs: append(append([]slog.Attr(nil), handler.attrs...), attrs...)}
}

func (handler recordedLogHandler) WithGroup(string) slog.Handler { return handler }
