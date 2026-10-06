// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	envelopesigning "github.com/superdurable/dex-connectors-library/connectors/docusign/examples/envelope-signing/flow"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const exampleEnvelopeID = "93be49ab-0000-0000-0000-000000000001"

func TestNewLoggerUsesLogLevel(t *testing.T) {
	for _, test := range []struct {
		levelName      string
		isDebugEnabled bool
		hasWarning     bool
	}{{"", false, false}, {"debug", true, false}, {" WARN ", false, false}, {"verbose", false, true}} {
		var output bytes.Buffer
		logger := newLogger(&output, test.levelName)
		require.Equal(t, test.isDebugEnabled, logger.Enabled(context.Background(), slog.LevelDebug), test.levelName)
		require.Equal(t, test.hasWarning, bytes.Contains(output.Bytes(), []byte("LOG_LEVEL is not debug")), test.levelName)
	}
}

func TestSigningPolicyReadsDurationsFromTheEnvironment(t *testing.T) {
	t.Setenv("SIGNING_STATUS_POLL_INTERVAL", "")
	t.Setenv("SIGNING_DEADLINE", "")
	policy, err := signingPolicyFromEnvironment()
	require.NoError(t, err)
	require.Equal(t, envelopesigning.DefaultSigningPolicy(), policy)

	t.Setenv("SIGNING_STATUS_POLL_INTERVAL", "12h")
	t.Setenv("SIGNING_DEADLINE", "720h")
	policy, err = signingPolicyFromEnvironment()
	require.NoError(t, err)
	require.Equal(t, 12*time.Hour, policy.StatusPollInterval)
	require.Equal(t, 30*24*time.Hour, policy.SigningDeadline)

	t.Setenv("SIGNING_DEADLINE", "two weeks")
	_, err = signingPolicyFromEnvironment()
	require.ErrorContains(t, err, "SIGNING_DEADLINE")
}

func TestDirectoryDocumentStoreReplacesWholeFilesAndKeepsNothingFromAFailedDownload(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "signed")
	store, err := newDirectoryDocumentStore(directory)
	require.NoError(t, err)
	reference := docusign.CombinedDocumentReference{AccountID: exampleAccountID, EnvelopeID: exampleEnvelopeID, IncludesCertificate: true}

	location, err := store.StoreCombinedDocument(context.Background(), reference, bytes.NewReader([]byte("%PDF-first")))
	require.NoError(t, err)
	require.Equal(t, exampleAccountID+"-"+exampleEnvelopeID+"-with-certificate.pdf", location)
	location, err = store.StoreCombinedDocument(context.Background(), reference, bytes.NewReader(signedPDF))
	require.NoError(t, err)
	stored, err := os.ReadFile(filepath.Join(directory, location))
	require.NoError(t, err)
	require.Equal(t, signedPDF, stored, "a repeated download replaces the file whole")

	interrupted := io.MultiReader(bytes.NewReader([]byte("%PDF-partial")), iotest.ErrReader(errors.New("connection reset")))
	_, err = store.StoreCombinedDocument(context.Background(), reference, interrupted)
	require.Error(t, err)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 1, "an interrupted download leaves no partial file")
	stored, err = os.ReadFile(filepath.Join(directory, location))
	require.NoError(t, err)
	require.Equal(t, signedPDF, stored, "the earlier complete copy survives")
}

// TestConnectEndpointAnswersWhileDexIsUnreachable proves the endpoint verifies and filters Connect
// deliveries without Dex, and retries the accepted outcome toward Dex.
func TestConnectEndpointAnswersWhileDexIsUnreachable(t *testing.T) {
	unreachableDex := "unix://" + filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano()))
	logs := &recordedLogs{}
	fake := newFakeDocuSign(t)
	connection := newExampleConnection(t, fake, docusign.WithLogger(logs.logger()))
	flow, err := envelopesigning.NewEnvelopeSigningFlow(connection, envelopesigning.DefaultSigningPolicy())
	require.NoError(t, err)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: unreachableDex})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	endpoint := newConnectEndpoint(t, connection, newEnvelopeEventTarget(client, flow, logs.logger()))

	completed := connectEventBody(docusign.ConnectEventEnvelopeCompleted, exampleEnvelopeID, "opp-123")
	require.Equal(t, http.StatusOK, endpoint.deliver(t, completed, sentinelHMACKey))
	require.Equal(t, http.StatusBadRequest, endpoint.deliver(t, completed, "another-key"))
	recipientEvent := connectEventBody("recipient-completed", exampleEnvelopeID, "opp-123")
	require.Equal(t, http.StatusOK, endpoint.deliver(t, recipientEvent, sentinelHMACKey), "an event the Trigger ignores is acknowledged")
	eventID := exampleEnvelopeID + ":" + docusign.ConnectEventEnvelopeCompleted
	require.Eventually(t, func() bool {
		return logs.count("trigger delivery failed; retrying", map[string]string{"event_id": eventID}) > 0
	}, 10*time.Second, 25*time.Millisecond, "the acknowledged outcome is retried toward Dex")
	require.NotContains(t, logs.text(), sentinelHMACKey)
	require.NotContains(t, logs.text(), sentinelToken)
}

func TestConnectEndpointAnswersRetryableBeforeTheBindingRuns(t *testing.T) {
	connection := newExampleConnection(t, newFakeDocuSign(t))
	handler, err := connection.EnvelopeEventReceivedWebhookHandler()
	require.NoError(t, err)
	body := connectEventBody(docusign.ConnectEventEnvelopeCompleted, exampleEnvelopeID, "opp-123")
	request := httptest.NewRequest(http.MethodPost, connectPath, bytes.NewReader([]byte(body)))
	request.Header.Set("X-DocuSign-Signature-1", connectSignature(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code, "Connect retries while no binding runs")
}
