// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	issueevents "github.com/superdurable/dex-connectors-library/connectors/linear/examples/issue-events/flow"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

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

func TestConnectionOptionsRedirectOnlyWhenTheLocalVariableIsSet(t *testing.T) {
	t.Setenv(localAPIURLEnvironmentVariable, "")
	require.Empty(t, connectionOptions())
	t.Setenv(localAPIURLEnvironmentVariable, "http://127.0.0.1:8899/graphql")
	require.Len(t, connectionOptions(), 1)
}

// TestEndpointAnswersDeliveriesWhileDexIsUnreachable proves the endpoint verifies and filters deliveries
// without Dex, and retries the accepted issue toward Dex.
func TestEndpointAnswersDeliveriesWhileDexIsUnreachable(t *testing.T) {
	unreachableDex := "unix://" + filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano()))
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, linear.WithLogger(logs.logger()))
	flow := issueevents.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: unreachableDex})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	endpoint := newIssueEndpoint(t, connection, newIssueTarget(client, flow, logs.logger()))
	endpoint.start(t)
	acceptedID := "11111111-1111-4111-8111-000000000001"
	accepted := triggerEventID("create", acceptedID)

	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, issueWebhookBody("create", acceptedID, exampleTeamID, time.Now()), sentinelSigningSecret, false))
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		issueWebhookBody("create", "11111111-1111-4111-8111-000000000002", exampleTeamID, time.Now()), sentinelSigningSecret, true))
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		issueWebhookBody("create", "11111111-1111-4111-8111-000000000003", exampleTeamID, time.Now()), "another-secret", false))
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t,
		issueWebhookBody("create", "11111111-1111-4111-8111-000000000004", otherTeamID, time.Now()), sentinelSigningSecret, false),
		"the binding filters another team but still acknowledges it")
	require.Eventually(t, func() bool {
		return len(logs.find("trigger delivery failed; retrying", map[string]string{"event_id": accepted})) > 0
	}, 10*time.Second, 25*time.Millisecond, "the acknowledged issue is retried toward Dex")
	for _, issueID := range []string{"11111111-1111-4111-8111-000000000002", "11111111-1111-4111-8111-000000000003", "11111111-1111-4111-8111-000000000004"} {
		require.Empty(t, logs.find("trigger delivery failed; retrying", map[string]string{"event_id": triggerEventID("create", issueID)}), issueID)
	}
	require.NotContains(t, logs.text(), sentinelSigningSecret)
	require.NotContains(t, logs.text(), sentinelAPIKey)
}
