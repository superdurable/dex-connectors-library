// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	envelopesigning "github.com/superdurable/dex-connectors-library/connectors/docusign/examples/envelope-signing/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// sentinelToken stands in for the access token; no log record or response may contain it.
	sentinelToken = "SENTINEL-DOCUSIGN-ACCESS-TOKEN"
	// sentinelHMACKey stands in for the Connect HMAC key.
	sentinelHMACKey = "SENTINEL-DOCUSIGN-CONNECT-HMAC-KEY"

	exampleAccountID  = "a4ec37d6-1111-2222-3333-143885c220e1"
	exampleTemplateID = "8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a"
)

// signedPDF stands in for the signed combined PDF with its certificate of completion.
var signedPDF = []byte("%PDF-1.7\n% signed Meridian MSA with certificate\n%%EOF\n")

// fakeDocuSign stands in for DocuSign over TLS: it creates, reports, voids, and serves the signed PDF.
type fakeDocuSign struct {
	server *httptest.Server
	mu     sync.Mutex
	// statuses holds each envelope's current status; envelopes are numbered in creation order.
	statuses    map[string]string
	markers     map[string]string
	creates     int
	statusReads int
	downloads   int
	voidBodies  []string
}

func newFakeDocuSign(t *testing.T) *fakeDocuSign {
	t.Helper()
	fake := &fakeDocuSign{statuses: map[string]string{}, markers: map[string]string{}}
	fake.server = httptest.NewTLSServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *fakeDocuSign) serve(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+sentinelToken {
		writeJSON(response, http.StatusUnauthorized, `{"errorCode":"USER_AUTHENTICATION_FAILED"}`)
		return
	}
	accountPath := "/restapi/v2.1/accounts/" + exampleAccountID
	envelopeID, suffix, _ := strings.Cut(strings.TrimPrefix(request.URL.Path, accountPath+"/envelopes/"), "/")
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/oauth/userinfo":
		writeJSON(response, http.StatusOK, `{"accounts":[{"account_id":"`+exampleAccountID+`","is_default":true,"base_uri":"https://na3.docusign.net"}]}`)
	case request.Method == http.MethodPost && request.URL.Path == accountPath+"/envelopes":
		fake.createEnvelope(response, request)
	case request.Method == http.MethodGet && suffix == "" && envelopeID != "":
		fake.mu.Lock()
		fake.statusReads++
		status, marker := fake.statuses[envelopeID], fake.markers[envelopeID]
		fake.mu.Unlock()
		writeJSON(response, http.StatusOK, `{"envelopeId":"`+envelopeID+`","status":"`+status+`","customFields":{"textCustomFields":[{"name":"dexIdempotencyKey","value":"`+marker+`"}]}}`)
	case request.Method == http.MethodGet && suffix == "documents/combined":
		fake.serveDocument(response, request)
	case request.Method == http.MethodPut && suffix == "" && envelopeID != "":
		body, _ := io.ReadAll(request.Body) // An unreadable body is recorded empty and fails the assertion.
		fake.mu.Lock()
		fake.voidBodies = append(fake.voidBodies, string(body))
		fake.statuses[envelopeID] = "voided"
		fake.mu.Unlock()
		writeJSON(response, http.StatusOK, `{"envelopeId":"`+envelopeID+`"}`)
	default:
		writeJSON(response, http.StatusNotFound, `{"errorCode":"RESOURCE_NOT_FOUND"}`)
	}
}

func (fake *fakeDocuSign) createEnvelope(response http.ResponseWriter, request *http.Request) {
	var body struct {
		TemplateID   string `json:"templateId"`
		CustomFields struct {
			TextCustomFields []struct{ Name, Value string } `json:"textCustomFields"`
		} `json:"customFields"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.TemplateID != exampleTemplateID {
		writeJSON(response, http.StatusBadRequest, `{"errorCode":"TEMPLATE_ID_INVALID"}`)
		return
	}
	fake.mu.Lock()
	fake.creates++
	envelopeID := fmt.Sprintf("93be49ab-0000-0000-0000-%012d", fake.creates)
	fake.statuses[envelopeID] = "sent"
	fake.markers[envelopeID] = body.CustomFields.TextCustomFields[0].Value
	fake.mu.Unlock()
	writeJSON(response, http.StatusCreated, `{"envelopeId":"`+envelopeID+`","status":"sent","statusDateTime":"2026-10-04T12:00:00.0000000Z"}`)
}

// serveDocument answers with the signed PDF.
func (fake *fakeDocuSign) serveDocument(response http.ResponseWriter, _ *http.Request) {
	fake.mu.Lock()
	fake.downloads++
	fake.mu.Unlock()
	response.Header().Set("Content-Type", "application/pdf")
	response.Header().Set("Content-Length", strconv.Itoa(len(signedPDF)))
	_, _ = response.Write(signedPDF) // The connector retries an interrupted download.
}

func (fake *fakeDocuSign) setStatus(envelopeID string, status string) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.statuses[envelopeID] = status
}

func (fake *fakeDocuSign) counts() (creates int, statusReads int, downloads int, voids []string) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.creates, fake.statusReads, fake.downloads, append([]string(nil), fake.voidBodies...)
}

// redirectingClient sends every request, whatever its host, to the fake server.
func (fake *fakeDocuSign) redirectingClient() *http.Client {
	target, err := url.Parse(fake.server.URL)
	if err != nil {
		panic(err)
	}
	base := fake.server.Client().Transport
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.URL.Scheme, redirected.URL.Host, redirected.Host = target.Scheme, target.Host, target.Host
		return base.RoundTrip(redirected)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, _ = io.WriteString(response, body) // A failed write leaves the connector to classify a broken answer.
}

// newExampleConnection builds the connection Dex Web saves; a static credential replaces project storage.
func newExampleConnection(t *testing.T, fake *fakeDocuSign, options ...docusign.Option) docusign.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: docusign.ConnectorID, Name: envelopesigning.ConnectionName}
	options = append([]docusign.Option{docusign.WithHTTPClient(fake.redirectingClient())}, options...)
	client, err := docusign.New(docusign.Config{}, sdkgo.StaticCredentialProvider[docusign.Credentials]{reference: {
		AuthMethodID: docusign.ProductionOAuthAuthMethodID, OAuthClientID: "integration-key",
		OAuthClientSecret: sdkgo.NewSecretString("SENTINEL-CLIENT-SECRET"), AccessToken: sdkgo.NewSecretString(sentinelToken),
		RefreshToken: sdkgo.NewSecretString("SENTINEL-REFRESH-TOKEN"), ConnectHMACKey: sdkgo.NewSecretString(sentinelHMACKey),
	}}, options...)
	require.NoError(t, err)
	connection, err := docusign.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

// connectEndpoint serves the example's target like NewProjectEnvelopeEventReceivedEndpointRunner, without
// the durable project inbox.
type connectEndpoint struct {
	server         *httptest.Server
	endpointRunner *webhooktrigger.EndpointRunner
}

func newConnectEndpoint(t *testing.T, connection docusign.Connection, target sdkgo.TriggerTarget[docusign.EnvelopeEvent]) *connectEndpoint {
	t.Helper()
	handler, err := connection.EnvelopeEventReceivedWebhookHandler()
	require.NoError(t, err)
	trigger := docusign.NewEnvelopeEventReceivedTrigger(docusign.EnvelopeEventReceivedTriggerConfig{
		Connection: connection, ConnectionName: envelopesigning.ConnectionName,
		BindingName: envelopesigning.EnvelopeOutcomeTriggerBinding, Target: target,
	})
	endpointRunner, err := webhooktrigger.NewEndpointRunner(handler, trigger)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(connectPath, endpointRunner)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- endpointRunner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-finished, context.Canceled)
	})
	readiness := handler.(interface{ RunningSourceCount() int })
	require.Eventually(t, func() bool { return readiness.RunningSourceCount() == 1 }, 10*time.Second, 10*time.Millisecond)
	return &connectEndpoint{server: server, endpointRunner: endpointRunner}
}

// deliver posts body as Connect does, signed with hmacKey in X-DocuSign-Signature-1.
func (endpoint *connectEndpoint) deliver(t *testing.T, body string, hmacKey string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint.server.URL+connectPath, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-DocuSign-Signature-1", connectSignatureWithKey(hmacKey, body))
	response, err := endpoint.server.Client().Do(request)
	require.NoError(t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.NotContains(t, string(responseBody), sentinelHMACKey)
	return response.StatusCode
}

// connectSignature signs body with the connection's Connect HMAC key, as DocuSign does.
func connectSignature(body string) string {
	return connectSignatureWithKey(sentinelHMACKey, body)
}

// connectSignatureWithKey is the base64 HMAC-SHA256 of the whole body.
func connectSignatureWithKey(hmacKey string, body string) string {
	mac := hmac.New(sha256.New, []byte(hmacKey))
	_, _ = mac.Write([]byte(body)) // A hash never fails to write.
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// connectEventBody is a JSON SIM envelope event whose summary carries the correlation custom field.
func connectEventBody(event string, envelopeID string, requestID string) string {
	customFields := ""
	if requestID != "" {
		customFields = `,"customFields":{"textCustomFields":[{"name":"` + envelopesigning.CorrelationCustomFieldName + `","value":"` + requestID + `"}]}`
	}
	return `{"event":"` + event + `","apiVersion":"v2.1","retryCount":0,"configurationId":1,"generatedDateTime":"2026-10-04T13:00:00.0000000Z",` +
		`"data":{"accountId":"` + exampleAccountID + `","envelopeId":"` + envelopeID + `","envelopeSummary":{"status":"` +
		strings.TrimPrefix(event, "envelope-") + `"` + customFields + `}}}`
}

// recordedLogs captures slog records so tests can assert delivery outcomes without fixed sleeps.
type recordedLogs struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (logs *recordedLogs) Write(contents []byte) (int, error) {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	return logs.buffer.Write(contents)
}

func (logs *recordedLogs) logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func (logs *recordedLogs) text() string {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	return logs.buffer.String()
}

// count returns how many records have message and every given attribute value.
func (logs *recordedLogs) count(message string, attributes map[string]string) int {
	matches := 0
	for _, line := range strings.Split(logs.text(), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil || record["msg"] != message {
			continue
		}
		isMatch := true
		for key, value := range attributes {
			if fmt.Sprint(record[key]) != value {
				isMatch = false
			}
		}
		if isMatch {
			matches++
		}
	}
	return matches
}
