//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package changeddeals

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
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm/internal/fakecrm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAccessToken = "1000.zohoCRMIntegrationAccess0123456789"

var feedStart = time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC)

// seedDeals stores count deals; deals 45 to 54 share one second, so a page boundary falls inside a tie.
func seedDeals(provider *fakecrm.Server, count int) []string {
	var dealIDs []string
	for index := 0; index < count; index++ {
		modifiedAt := feedStart.Add(time.Duration(index) * time.Second)
		if index >= 45 && index < 55 {
			modifiedAt = feedStart.Add(45 * time.Second)
		}
		dealIDs = append(dealIDs, provider.SeedRecord("Deals", map[string]any{
			"Deal_Name": "Deal " + strconv.Itoa(index), "Stage": "Qualification",
		}, modifiedAt))
	}
	provider.SeedRecord("Deals", map[string]any{"Deal_Name": "Before the feed", "Stage": "Closed Won"}, feedStart.Add(-time.Second))
	return dealIDs
}

func TestFeedPagesThroughSameSecondTiesWithoutSkippingOrRepeatingWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	dealIDs := seedDeals(provider, 60)
	harness := newFeedHarness(t, provider)

	changed := harness.runFeed(t, "ties", Input{ModifiedSince: feedStart.Format(time.RFC3339)})
	require.Equal(t, 2, changed.Pages)
	require.False(t, changed.HasMore)
	require.Equal(t, dealIDs, changedDealIDs(changed), "every deal once, oldest change first, the earlier deal excluded")
	require.Equal(t, "2026-01-20T09:00:59Z/"+dealIDs[59], changed.Cursor)
	queries := provider.RequestsNamed("coql")
	require.Len(t, queries, 2)
	require.Equal(t, "select Deal_Name, Stage, Modified_Time from Deals where Modified_Time >= '2026-01-20T09:00:00+00:00'"+
		" order by Modified_Time asc, id asc limit 0, 50", queries[0].SelectQuery)
	require.Equal(t, "select Deal_Name, Stage, Modified_Time from Deals where (Modified_Time > '2026-01-20T09:00:45+00:00' or "+
		"(Modified_Time = '2026-01-20T09:00:45+00:00' and id > "+dealIDs[49]+")) order by Modified_Time asc, id asc limit 0, 50",
		queries[1].SelectQuery, "the second page continues inside the same-second tie")
}

// TestADealChangedWhileTheFeedIsReadIsNeverSkippedWithRealDex moves a deal of the first page to the
// end of the feed after that page was read; offset paging would then skip an unchanged deal.
func TestADealChangedWhileTheFeedIsReadIsNeverSkippedWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	dealIDs := seedDeals(provider, 60)
	provider.TouchesAfterFirstCOQL = dealIDs[3]
	harness := newFeedHarness(t, provider)

	changed := harness.runFeed(t, "moved", Input{ModifiedSince: feedStart.Format(time.RFC3339)})
	readIDs := changedDealIDs(changed)
	require.Equal(t, append(append([]string(nil), dealIDs...), dealIDs[3]), readIDs, "the changed deal appears again after its change")
	require.True(t, strings.HasSuffix(changed.Cursor, "/"+dealIDs[3]))
}

func TestASecondRunContinuesFromTheFirstRunsCursorWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	dealIDs := seedDeals(provider, 60)
	harness := newFeedHarness(t, provider)

	first := harness.runFeed(t, "first-run", Input{ModifiedSince: feedStart.Format(time.RFC3339), MaxPages: 1})
	require.True(t, first.HasMore, "the page budget ended before the feed")
	require.Len(t, first.Deals, PageLimit)
	second := harness.runFeed(t, "second-run", Input{Cursor: first.Cursor})
	require.False(t, second.HasMore)
	require.Equal(t, dealIDs, append(changedDealIDs(first), changedDealIDs(second)...))

	idle := harness.runFeed(t, "idle-poll", Input{Cursor: second.Cursor})
	require.Empty(t, idle.Deals)
	require.Equal(t, second.Cursor, idle.Cursor, "an idle poll keeps the watermark")
}

func TestInvalidFeedStartFailsBeforeCallingZohoCRMWithRealDex(t *testing.T) {
	provider := fakecrm.New(t, integrationAccessToken)
	harness := newFeedHarness(t, provider)
	flowID := harness.startFeed(t, "invalid", Input{Cursor: "page_token_c8582xx9e7c7"})
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.TotalRequests())
}

func changedDealIDs(changed ChangedDeals) []string {
	var dealIDs []string
	for _, deal := range changed.Deals {
		dealIDs = append(dealIDs, deal.ID)
	}
	return dealIDs
}

// feedHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type feedHarness struct {
	flow   *Flow
	client *dex.Client
}

func newFeedHarness(t *testing.T, provider *fakecrm.Server) *feedHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "zoho", Name: ConnectionName}
	credentials := crm.Credentials{
		AuthMethodID: crm.USDataCenterAuthMethodID, OAuthClientID: "1000.ZOHOCRMINTEGRATIONCLIENT",
		OAuthClientSecret: sdkgo.NewSecretString("zoho-integration-secret"), AccessToken: sdkgo.NewSecretString(integrationAccessToken),
		RefreshToken: sdkgo.NewSecretString("1000.zohoCRMIntegrationRefresh0123456789"), APIDomain: "https://www.zohoapis.com",
	}
	providerClient, err := crm.New(crm.Config{}, sdkgo.StaticCredentialProvider[crm.Credentials]{reference: credentials},
		crm.WithAPIBaseURL(provider.URL+"/crm/v8"), crm.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}))
	require.NoError(t, err)
	connection, err := crm.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness := &feedHarness{flow: NewFlow(connection)}
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	registry, err := dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, harness.client.Close(), cache.Close()))
	})
	return harness
}

func (harness *feedHarness) runFeed(t *testing.T, scenario string, input Input) ChangedDeals {
	t.Helper()
	flowID := harness.startFeed(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var changed ChangedDeals
	require.NoError(t, result.DecodeSingleOutput(&changed))
	return changed
}

func (harness *feedHarness) startFeed(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "zoho-crm-changed-deals-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s", flowID)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *feedHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
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
