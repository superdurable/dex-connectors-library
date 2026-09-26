// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	"github.com/superdurable/dex-connectors-library/connectors/google/gmail/internal/testlog"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

const (
	gmailLocalConnection = "gmail-inbox"
	gmailRootBinding     = "gmail-thread-start"
	gmailReplyBinding    = "gmail-thread-reply"
)

func TestReplyTriggerConsumesUndeliverableAndDeliversRestOfPage(t *testing.T) {
	// Gmail lists newest first, so reply-1 is the oldest message and the first one scanned.
	page := []gmailPageMessage{{"reply-3", "thread-1", true}, {"reply-2", "thread-1", true}, {"reply-1", "thread-1", true}}
	newTarget := func(delivered chan<- string) sdkgo.TriggerTarget[gmail.MessageEvent] {
		return sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[gmail.MessageEvent]) error {
			if event.ID == "reply-1" {
				return sdkgo.MarkTriggerUndeliverable(errors.New("the thread's Flow already completed"))
			}
			delivered <- event.ID
			return nil
		})
	}

	t.Run("generated Trigger factory", func(t *testing.T) {
		provider := newGmailPageProvider(t, page)
		client, err := gmail.New(gmail.Config{Endpoint: provider.URL, PollInterval: time.Second}, sdkgo.StaticCredentialProvider[gmail.Credentials]{
			gmailConnection: {AccessToken: sdkgo.NewSecretString("gmail-token"), PrimaryEmail: "owner@example.com"},
		})
		require.NoError(t, err)
		connection, err := gmail.NewConnection(client, gmailConnection)
		require.NoError(t, err)
		delivered := make(chan string, 8)
		runner := gmail.NewReplyReceivedTrigger(gmail.ReplyReceivedTriggerConfig{
			Connection: connection, BindingName: gmailReplyBinding, Target: newTarget(delivered),
		})
		stop := runInBackground(t, runner.Run)
		require.Equal(t, []string{"reply-2", "reply-3"}, receiveEventIDs(t, delivered, 2))
		require.Equal(t, int32(1), provider.listCount(""))
		stop()
	})

	t.Run("shared runner without a durable inbox", func(t *testing.T) {
		provider := newGmailPageProvider(t, page)
		logs := testlog.NewLogRecorder()
		client, err := gmail.New(gmail.Config{Endpoint: provider.URL, PollInterval: time.Second}, sdkgo.StaticCredentialProvider[gmail.Credentials]{
			gmailConnection: {AccessToken: sdkgo.NewSecretString(sentinelAccessToken), PrimaryEmail: "owner@example.com"},
		}, gmail.WithLogger(logs.Logger()))
		require.NoError(t, err)
		connection, err := gmail.NewConnection(client, gmailConnection)
		require.NoError(t, err)
		delivered := make(chan string, 8)
		runner, err := gmail.NewMessageTriggerRunner(gmail.MessageTriggerRunnerConfig{
			Connection:          connection,
			ReplyReceivedRoutes: []gmail.ReplyReceivedTriggerRoute{{BindingName: gmailReplyBinding, Target: newTarget(delivered)}},
		})
		require.NoError(t, err)
		stop := runInBackground(t, runner.Run)
		require.Equal(t, []string{"reply-2", "reply-3"}, receiveEventIDs(t, delivered, 2))
		require.Equal(t, int32(1), provider.listCount(""))
		stop()
		// Without a durable inbox, the poller itself consumes and logs the undeliverable reply.
		skipped := logs.Find("trigger event skipped: undeliverable", nil)
		require.Len(t, skipped, 1)
		require.Equal(t, slog.LevelWarn, skipped[0].Level)
		require.Equal(t, map[string]string{
			"connector": "gmail", "connection": gmailConnection.Name, "trigger": "replyReceived", "binding": gmailReplyBinding,
			"event_id": "reply-1", "thread_id": "thread-1",
			"error": "Trigger event is undeliverable: the thread's Flow already completed",
		}, skipped[0].Attrs)
		require.Empty(t, logs.Find("trigger delivery failed; retrying", nil))
		require.NotContains(t, logs.Text(), "SENTINEL")
	})

	t.Run("local shared runner", func(t *testing.T) {
		provider := newGmailPageProvider(t, page)
		store, directory := newGmailLocalStore(t, provider.URL)
		logs := testlog.NewLogRecorder()
		delivered := make(chan string, 8)
		runner, err := gmail.NewLocalMessageTriggerRunner(store, gmailLocalConnection, gmail.LocalMessageTriggerRunnerConfig{
			ReplyReceivedRoutes: []gmail.LocalReplyReceivedTriggerRoute{{BindingName: gmailReplyBinding, Target: newTarget(delivered)}},
		}, gmail.WithLogger(logs.Logger()))
		require.NoError(t, err)
		stop := runInBackground(t, runner.Run)
		require.Equal(t, []string{"reply-2", "reply-3"}, receiveEventIDs(t, delivered, 2))
		stop()
		require.Empty(t, pendingGmailEventIDs(t, directory))
		// The durable inbox consumes the undeliverable reply and logs it once, with the thread the poller
		// passes along; the poller does not repeat it.
		skipped := logs.Find("trigger event skipped: undeliverable", nil)
		require.Len(t, skipped, 1)
		require.Equal(t, map[string]string{
			"connector": "gmail", "connection": gmailLocalConnection, "trigger": "replyReceived", "binding": gmailReplyBinding,
			"thread_id": "thread-1", "event_id": "reply-1", "error": "Trigger event is undeliverable: the thread's Flow already completed",
		}, skipped[0].Attrs)
		require.Empty(t, logs.Find("trigger delivered after retry", nil))
		require.NotContains(t, logs.Text(), "SENTINEL")
	})

	t.Run("local durable Trigger factory", func(t *testing.T) {
		provider := newGmailPageProvider(t, page)
		store, directory := newGmailLocalStore(t, provider.URL)
		delivered := make(chan string, 8)
		runner, err := gmail.NewLocalReplyReceivedTrigger(store, gmailLocalConnection, gmailReplyBinding, newTarget(delivered))
		require.NoError(t, err)
		stop := runInBackground(t, runner.Run)
		require.Equal(t, []string{"reply-2", "reply-3"}, receiveEventIDs(t, delivered, 2))
		stop()
		require.Empty(t, pendingGmailEventIDs(t, directory))

		var replayCalls atomic.Int32
		restarted, err := localconfig.NewDurableTriggerTarget(store, gmail.ConnectorID, gmailLocalConnection, "replyReceived", gmailReplyBinding,
			sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(context.Context, sdkgo.TriggerEvent[gmail.MessageEvent]) error {
				replayCalls.Add(1)
				return nil
			}))
		require.NoError(t, err)
		replayer, ok := restarted.(sdkgo.TriggerDeliveryReplayer)
		require.True(t, ok)
		require.NoError(t, replayer.ReplayTriggerDeliveries(context.Background()))
		require.Zero(t, replayCalls.Load())
	})
}

// TestPollerRetriesFailedMessageAfterNewerMailPushesItOffThePage covers a message whose delivery fails
// while newer mail pushes it off the listed page. The next poll still retries it, before the newer
// message, and logs the retry and the recovery.
func TestPollerRetriesFailedMessageAfterNewerMailPushesItOffThePage(t *testing.T) {
	pages := [][]gmailPageMessage{{{"root-a", "thread-a", false}}, {{"root-b", "thread-b", false}}}
	newTarget := func(deliveries *orderedDeliveries) sdkgo.TriggerTarget[gmail.MessageEvent] {
		var attempts atomic.Int32
		return sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[gmail.MessageEvent]) error {
			if event.ID == "root-a" && attempts.Add(1) == 1 {
				return errors.New("Dex is unavailable")
			}
			deliveries.record("root", event.ID)
			return nil
		})
	}
	requireRetryRecords := func(t *testing.T, logs *testlog.LogRecorder, connectionName string) {
		t.Helper()
		identity := map[string]string{
			"connector": "gmail", "connection": connectionName, "trigger": "messageReceived", "binding": gmailRootBinding,
			"thread_id": "thread-a", "event_id": "root-a",
		}
		retries := logs.Find("trigger delivery failed; retrying", nil)
		require.Len(t, retries, 1)
		require.Equal(t, slog.LevelWarn, retries[0].Level)
		require.Equal(t, withGmailAttrs(identity, "attempt", "1", "delay", "1s", "error", "Dex is unavailable"), retries[0].Attrs)
		recovered := logs.Find("trigger delivered after retry", nil)
		require.Len(t, recovered, 1)
		require.Equal(t, slog.LevelInfo, recovered[0].Level)
		require.Equal(t, withGmailAttrs(identity, "attempts", "2"), recovered[0].Attrs)
		require.Empty(t, logs.Find("trigger event skipped: undeliverable", nil))
	}

	t.Run("shared runner without a durable inbox", func(t *testing.T) {
		provider := newGmailShiftingProvider(t, pages)
		logs := testlog.NewLogRecorder()
		client, err := gmail.New(gmail.Config{Endpoint: provider.URL, PollInterval: time.Second}, sdkgo.StaticCredentialProvider[gmail.Credentials]{
			gmailConnection: {AccessToken: sdkgo.NewSecretString(sentinelAccessToken), PrimaryEmail: "owner@example.com"},
		}, gmail.WithLogger(logs.Logger()))
		require.NoError(t, err)
		connection, err := gmail.NewConnection(client, gmailConnection)
		require.NoError(t, err)
		deliveries := &orderedDeliveries{}
		runner, err := gmail.NewMessageTriggerRunner(gmail.MessageTriggerRunnerConfig{
			Connection:            connection,
			MessageReceivedRoutes: []gmail.MessageReceivedTriggerRoute{{BindingName: gmailRootBinding, Target: newTarget(deliveries)}},
		})
		require.NoError(t, err)
		stop := runInBackground(t, runner.Run)
		require.Eventually(t, func() bool { return len(deliveries.snapshot()) == 2 }, 8*time.Second, 10*time.Millisecond)
		stop()
		require.Equal(t, []string{"root:root-a", "root:root-b"}, deliveries.snapshot())
		requireRetryRecords(t, logs, gmailConnection.Name)
		require.NotContains(t, logs.Text(), "SENTINEL")
	})

	t.Run("local shared runner", func(t *testing.T) {
		provider := newGmailShiftingProvider(t, pages)
		store, directory := newGmailLocalStore(t, provider.URL)
		logs := testlog.NewLogRecorder()
		deliveries := &orderedDeliveries{}
		runner, err := gmail.NewLocalMessageTriggerRunner(store, gmailLocalConnection, gmail.LocalMessageTriggerRunnerConfig{
			MessageReceivedRoutes: []gmail.LocalMessageReceivedTriggerRoute{{BindingName: gmailRootBinding, Target: newTarget(deliveries)}},
		}, gmail.WithLogger(logs.Logger()))
		require.NoError(t, err)
		stop := runInBackground(t, runner.Run)
		require.Eventually(t, func() bool { return len(deliveries.snapshot()) == 2 }, 8*time.Second, 10*time.Millisecond)
		stop()
		require.Equal(t, []string{"root:root-a", "root:root-b"}, deliveries.snapshot())
		require.Empty(t, pendingGmailEventIDs(t, directory), "the retried message leaves the inbox without a restart")
		requireRetryRecords(t, logs, gmailLocalConnection)
	})
}

// TestPollerDoesNotReportAnInboxSkipAsDeliveredAfterRetry covers a message that fails once and is
// consumed as undeliverable on the next poll, as when its Flow completes during a Dex outage. The
// component that consumes it logs the skip, and the poller does not also report it as delivered.
func TestPollerDoesNotReportAnInboxSkipAsDeliveredAfterRetry(t *testing.T) {
	page := []gmailPageMessage{{"reply-1", "thread-1", true}}
	newTarget := func(attempts *atomic.Int32) sdkgo.TriggerTarget[gmail.MessageEvent] {
		return sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(context.Context, sdkgo.TriggerEvent[gmail.MessageEvent]) error {
			if attempts.Add(1) == 1 {
				return errors.New("Dex is unavailable")
			}
			return sdkgo.MarkTriggerUndeliverable(errors.New("the thread's Flow already completed"))
		})
	}

	t.Run("local shared runner", func(t *testing.T) {
		provider := newGmailPageProvider(t, page)
		store, directory := newGmailLocalStore(t, provider.URL)
		logs := testlog.NewLogRecorder()
		var attempts atomic.Int32
		runner, err := gmail.NewLocalMessageTriggerRunner(store, gmailLocalConnection, gmail.LocalMessageTriggerRunnerConfig{
			ReplyReceivedRoutes: []gmail.LocalReplyReceivedTriggerRoute{{BindingName: gmailReplyBinding, Target: newTarget(&attempts)}},
		}, gmail.WithLogger(logs.Logger()))
		require.NoError(t, err)
		stop := runInBackground(t, runner.Run)
		require.Eventually(t, func() bool { return len(logs.Find("trigger event skipped: undeliverable", nil)) == 1 }, 8*time.Second, 10*time.Millisecond)
		stop()
		require.Equal(t, int32(2), attempts.Load())
		require.Empty(t, pendingGmailEventIDs(t, directory))
		require.Len(t, logs.Find("trigger delivery failed; retrying", nil), 1)
		skipped := logs.Find("trigger event skipped: undeliverable", nil)
		require.Equal(t, map[string]string{
			"connector": "gmail", "connection": gmailLocalConnection, "trigger": "replyReceived", "binding": gmailReplyBinding,
			"thread_id": "thread-1", "event_id": "reply-1", "error": "Trigger event is undeliverable: the thread's Flow already completed",
		}, skipped[0].Attrs)
		require.Empty(t, logs.Find("trigger delivered after retry", nil))
	})

	t.Run("local durable Trigger factory", func(t *testing.T) {
		provider := newGmailPageProvider(t, page)
		store, directory := newGmailLocalStore(t, provider.URL)
		logs := testlog.NewLogRecorder()
		var attempts atomic.Int32
		runner, err := gmail.NewLocalReplyReceivedTrigger(store, gmailLocalConnection, gmailReplyBinding, newTarget(&attempts),
			gmail.WithLogger(logs.Logger()))
		require.NoError(t, err)
		stop := runInBackground(t, runner.Run)
		require.Eventually(t, func() bool { return attempts.Load() == 2 }, 8*time.Second, 10*time.Millisecond)
		require.Eventually(t, func() bool { return len(pendingGmailEventIDs(t, directory)) == 0 }, 4*time.Second, 10*time.Millisecond)
		stop()
		// The factory's inbox logs the skip to slog.Default(); the poller logs only the failed attempt.
		require.Len(t, logs.Find("trigger delivery failed; retrying", nil), 1)
		require.Empty(t, logs.Find("trigger delivered after retry", nil))
	})
}

// TestPollFailuresCountAttemptsAndReportTheirSource covers the attempt count of consecutive failed polls,
// which starts again after a poll that succeeds, and the record source that a handler with AddSource sees.
func TestPollFailuresCountAttemptsAndReportTheirSource(t *testing.T) {
	var lists atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/users/me/messages" {
			http.NotFound(response, request)
			return
		}
		// The first, second, and fourth list calls fail.
		switch lists.Add(1) {
		case 1, 2, 4:
			http.Error(response, `{"error":{"code":503}}`, http.StatusServiceUnavailable)
		default:
			_, _ = response.Write([]byte(`{"messages":[]}`))
		}
	}))
	t.Cleanup(provider.Close)
	var output strings.Builder
	client, err := gmail.New(gmail.Config{Endpoint: provider.URL, PollInterval: time.Second}, sdkgo.StaticCredentialProvider[gmail.Credentials]{
		gmailConnection: {AccessToken: sdkgo.NewSecretString(sentinelAccessToken), PrimaryEmail: "owner@example.com"},
	}, gmail.WithLogger(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{AddSource: true}))))
	require.NoError(t, err)
	connection, err := gmail.NewConnection(client, gmailConnection)
	require.NoError(t, err)
	runner, err := gmail.NewMessageTriggerRunner(gmail.MessageTriggerRunnerConfig{
		Connection: connection,
		MessageReceivedRoutes: []gmail.MessageReceivedTriggerRoute{{BindingName: gmailRootBinding,
			Target: sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(context.Context, sdkgo.TriggerEvent[gmail.MessageEvent]) error { return nil })}},
	})
	require.NoError(t, err)
	stop := runInBackground(t, runner.Run)
	require.Eventually(t, func() bool { return lists.Load() >= 5 }, 8*time.Second, 10*time.Millisecond)
	stop()

	attempts := []string{}
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var record struct {
			Message string `json:"msg"`
			Attempt int    `json:"attempt"`
			Source  struct {
				Function string `json:"function"`
			} `json:"source"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		require.Equal(t, "gmail poll failed; retrying", record.Message)
		require.Equal(t, "github.com/superdurable/dex-connectors-library/connectors/google/gmail.(*messagePollingTriggerSource).pollFailed",
			record.Source.Function)
		attempts = append(attempts, strconv.Itoa(record.Attempt))
	}
	require.Equal(t, []string{"1", "2", "1"}, attempts)
	require.NotContains(t, output.String(), "SENTINEL")
}

// withGmailAttrs copies attrs and adds the key-value pairs in extra.
func withGmailAttrs(attrs map[string]string, extra ...string) map[string]string {
	combined := make(map[string]string, len(attrs)+len(extra)/2)
	for key, value := range attrs {
		combined[key] = value
	}
	for index := 0; index+1 < len(extra); index += 2 {
		combined[extra[index]] = extra[index+1]
	}
	return combined
}

// newGmailShiftingProvider serves pages[n] for the nth list call, repeating the last page, so newer mail
// can push a message off the listed page. Every message on any page can be read.
func newGmailShiftingProvider(t *testing.T, pages [][]gmailPageMessage) *httptest.Server {
	t.Helper()
	messages := map[string]gmailPageMessage{}
	listings := make([][]byte, 0, len(pages))
	for _, page := range pages {
		references := make([]map[string]string, 0, len(page))
		for _, message := range page {
			messages[message.id] = message
			references = append(references, map[string]string{"id": message.id, "threadId": message.threadID})
		}
		listing, err := json.Marshal(map[string]any{"messages": references})
		require.NoError(t, err)
		listings = append(listings, listing)
	}
	var lists atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/users/me/messages" {
			_, _ = response.Write(listings[min(int(lists.Add(1))-1, len(listings)-1)])
			return
		}
		message, ok := messages[strings.TrimPrefix(request.URL.Path, "/users/me/messages/")]
		if !ok {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write([]byte(gmailMessageJSON(message.id, message.threadID, message.reply, "", "")))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestMessageTriggerRunnerDeliversRootsBeforeReplies(t *testing.T) {
	// The reply is newer, so Gmail lists it before its root in the same page.
	page := []gmailPageMessage{{"reply-1", "thread-1", true}, {"root-1", "thread-1", false}}

	t.Run("live poll", func(t *testing.T) {
		provider := newGmailPageProvider(t, page)
		store, _ := newGmailLocalStore(t, provider.URL)
		deliveries := &orderedDeliveries{}
		runner, err := gmail.NewLocalMessageTriggerRunner(store, gmailLocalConnection, deliveries.localConfig(nil))
		require.NoError(t, err)
		stop := runInBackground(t, runner.Run)
		require.Eventually(t, func() bool { return len(deliveries.snapshot()) == 2 }, 4*time.Second, 10*time.Millisecond)
		stop()
		require.Equal(t, []string{"root:root-1", "reply:reply-1"}, deliveries.snapshot())
	})

	t.Run("replay after restart", func(t *testing.T) {
		provider := newGmailPageProvider(t, nil)
		store, directory := newGmailLocalStore(t, provider.URL)
		// Persist the reply before its root, as a crash could leave them, to prove replay orders by route.
		for _, pending := range []struct {
			trigger string
			binding string
			message gmailPageMessage
		}{
			{"replyReceived", gmailReplyBinding, gmailPageMessage{"reply-1", "thread-1", true}},
			{"messageReceived", gmailRootBinding, gmailPageMessage{"root-1", "thread-1", false}},
		} {
			inbox, err := localconfig.NewDurableTriggerTarget(store, gmail.ConnectorID, gmailLocalConnection, pending.trigger, pending.binding,
				sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(context.Context, sdkgo.TriggerEvent[gmail.MessageEvent]) error { return nil }))
			require.NoError(t, err)
			require.NoError(t, sdkgo.PrepareTriggerDelivery(context.Background(), inbox, pending.message.event()))
		}
		deliveries := &orderedDeliveries{}
		runner, err := gmail.NewLocalMessageTriggerRunner(store, gmailLocalConnection, deliveries.localConfig(nil))
		require.NoError(t, err)
		stop := runInBackground(t, runner.Run)
		require.Eventually(t, func() bool { return len(deliveries.snapshot()) == 2 }, 4*time.Second, 10*time.Millisecond)
		stop()
		require.Equal(t, []string{"root:root-1", "reply:reply-1"}, deliveries.snapshot())
		require.Empty(t, pendingGmailEventIDs(t, directory))
	})

	t.Run("retryable root failure skips replies in that poll", func(t *testing.T) {
		provider := newGmailPageProvider(t, page)
		store, _ := newGmailLocalStore(t, provider.URL)
		deliveries := &orderedDeliveries{}
		var rootAttempts atomic.Int32
		logs := testlog.NewLogRecorder()
		runner, err := gmail.NewLocalMessageTriggerRunner(store, gmailLocalConnection, deliveries.localConfig(func() error {
			rootAttempts.Add(1)
			return errors.New("Dex is unavailable")
		}), gmail.WithLogger(logs.Logger()))
		require.NoError(t, err)
		stop := runInBackground(t, runner.Run)
		require.Eventually(t, func() bool { return rootAttempts.Load() >= 2 }, 4*time.Second, 10*time.Millisecond)
		stop()
		// Each poll lists the reply route first, but delivers no reply while a root is failing.
		require.Empty(t, deliveries.snapshot())
		require.GreaterOrEqual(t, provider.listCount("root-query"), int32(2))
		require.GreaterOrEqual(t, provider.listCount("reply-query"), int32(2))
		// Every failed poll logs the next attempt for the root and the poll interval as its delay.
		retries := logs.Find("trigger delivery failed; retrying", nil)
		require.GreaterOrEqual(t, len(retries), 2)
		for index, retry := range retries {
			require.Equal(t, slog.LevelWarn, retry.Level)
			require.Equal(t, map[string]string{
				"connector": "gmail", "connection": gmailLocalConnection, "trigger": "messageReceived", "binding": gmailRootBinding,
				"event_id": "root-1", "thread_id": "thread-1", "attempt": strconv.Itoa(index + 1), "delay": "1s", "error": "Dex is unavailable",
			}, retry.Attrs)
		}
		require.Empty(t, logs.Find("trigger delivered after retry", nil))
	})
}

// TestMessageTriggerRunnerDeliversRootBeforeReplyThatArrivesDuringPoll adds a root and its reply to the inbox
// between two list calls of one poll. The reply must never reach its target before the root.
func TestMessageTriggerRunnerDeliversRootBeforeReplyThatArrivesDuringPoll(t *testing.T) {
	for _, arriveAfter := range []string{"root-query", "reply-query"} {
		t.Run("arrival after the first "+arriveAfter+" listing", func(t *testing.T) {
			provider := newGmailArrivalProvider(t, arriveAfter,
				gmailPageMessage{"reply-1", "thread-1", true}, gmailPageMessage{"root-1", "thread-1", false})
			store, directory := newGmailLocalStore(t, provider.URL)
			var started atomic.Bool
			deliveries := &orderedDeliveries{}
			runner, err := gmail.NewLocalMessageTriggerRunner(store, gmailLocalConnection, gmail.LocalMessageTriggerRunnerConfig{
				MessageReceivedRoutes: []gmail.LocalMessageReceivedTriggerRoute{{
					BindingName: gmailRootBinding,
					Target: sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[gmail.MessageEvent]) error {
						started.Store(true)
						deliveries.record("root", event.ID)
						return nil
					}),
				}},
				ReplyReceivedRoutes: []gmail.LocalReplyReceivedTriggerRoute{{
					BindingName: gmailReplyBinding,
					// Like NewDexRPCTriggerTarget, a reply whose Flow has not started is undeliverable.
					Target: sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[gmail.MessageEvent]) error {
						if !started.Load() {
							deliveries.record("reply-undeliverable", event.ID)
							return sdkgo.MarkTriggerUndeliverable(errors.New("workflow not found for ID: thread-1"))
						}
						deliveries.record("reply", event.ID)
						return nil
					}),
				}},
			})
			require.NoError(t, err)
			stop := runInBackground(t, runner.Run)
			require.Eventually(t, func() bool { return len(deliveries.snapshot()) == 2 }, 5*time.Second, 10*time.Millisecond)
			stop()
			require.Equal(t, []string{"root:root-1", "reply:reply-1"}, deliveries.snapshot())
			require.Empty(t, pendingGmailEventIDs(t, directory))
		})
	}
}

// TestMessageTriggerRunnerLogsPollFailureRecoveryAndIgnoredMessages covers a failed list call, a root that
// is delivered on the poll after its first failure, and the reason each route ignores the other messages.
func TestMessageTriggerRunnerLogsPollFailureRecoveryAndIgnoredMessages(t *testing.T) {
	// Newest first: the reply, a root from a sender the root route does not allow, then the root.
	provider := newGmailSentinelProvider(t, []gmailSentinelMessage{
		{"reply-1", "thread-1", true, "Approver <approver@example.com>"},
		{"other-1", "thread-2", false, "Intruder <intruder@example.com>"},
		{"root-1", "thread-1", false, "Sender <sender@example.com>"},
	})
	logs := testlog.NewLogRecorder()
	client, err := gmail.New(gmail.Config{Endpoint: provider.URL, PollInterval: time.Second}, sdkgo.StaticCredentialProvider[gmail.Credentials]{
		gmailConnection: {AccessToken: sdkgo.NewSecretString(sentinelAccessToken), PrimaryEmail: "owner@example.com"},
	}, gmail.WithLogger(logs.Logger()))
	require.NoError(t, err)
	connection, err := gmail.NewConnection(client, gmailConnection)
	require.NoError(t, err)
	deliveries := &orderedDeliveries{}
	var rootAttempts atomic.Int32
	runner, err := gmail.NewMessageTriggerRunner(gmail.MessageTriggerRunnerConfig{
		Connection: connection,
		MessageReceivedRoutes: []gmail.MessageReceivedTriggerRoute{{
			BindingName:   gmailRootBinding,
			Configuration: gmail.MessageReceivedTriggerConfiguration{MessageMatcher: gmail.MessageMatcher{SenderEmails: []string{"sender@example.com"}}},
			Target: sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[gmail.MessageEvent]) error {
				if rootAttempts.Add(1) == 1 {
					return errors.New("Dex is unavailable")
				}
				deliveries.record("root", event.ID)
				return nil
			}),
		}},
		ReplyReceivedRoutes: []gmail.ReplyReceivedTriggerRoute{{
			BindingName: gmailReplyBinding,
			Target: sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[gmail.MessageEvent]) error {
				deliveries.record("reply", event.ID)
				return nil
			}),
		}},
	})
	require.NoError(t, err)
	stop := runInBackground(t, runner.Run)
	require.Eventually(t, func() bool { return len(deliveries.snapshot()) == 2 }, 8*time.Second, 10*time.Millisecond)
	stop()
	require.Equal(t, []string{"root:root-1", "reply:reply-1"}, deliveries.snapshot())

	identity := func(trigger string, binding string, extra ...string) map[string]string {
		attrs := map[string]string{"connector": "gmail", "connection": gmailConnection.Name, "trigger": trigger, "binding": binding}
		for index := 0; index+1 < len(extra); index += 2 {
			attrs[extra[index]] = extra[index+1]
		}
		return attrs
	}
	// The first poll's first list call, for the reply route, fails.
	pollFailures := logs.Find("gmail poll failed; retrying", nil)
	require.Len(t, pollFailures, 1)
	require.Equal(t, slog.LevelWarn, pollFailures[0].Level)
	require.Equal(t, identity("replyReceived", gmailReplyBinding, "attempt", "1", "delay", "1s", "error", "Gmail message list is unavailable"),
		pollFailures[0].Attrs)
	// The second poll fails to deliver the root, and the third delivers it.
	retries := logs.Find("trigger delivery failed; retrying", nil)
	require.Len(t, retries, 1)
	require.Equal(t, identity("messageReceived", gmailRootBinding,
		"event_id", "root-1", "thread_id", "thread-1", "attempt", "1", "delay", "1s", "error", "Dex is unavailable"), retries[0].Attrs)
	recovered := logs.Find("trigger delivered after retry", nil)
	require.Len(t, recovered, 1)
	require.Equal(t, slog.LevelInfo, recovered[0].Level)
	require.Equal(t, identity("messageReceived", gmailRootBinding, "event_id", "root-1", "thread_id", "thread-1", "attempts", "2"), recovered[0].Attrs)
	// Each route logs why it ignores every other message once, after the poll that completes.
	ignored := map[string]string{}
	for _, record := range logs.Find("trigger event ignored", nil) {
		require.Equal(t, slog.LevelDebug, record.Level)
		key := record.Attrs["binding"] + " " + record.Attrs["event_id"]
		require.NotContains(t, ignored, key, "an ignored message is logged once per route")
		ignored[key] = record.Attrs["reason"]
	}
	require.Equal(t, map[string]string{
		gmailRootBinding + " other-1":  "matcher_mismatch",
		gmailRootBinding + " reply-1":  "not_a_root",
		gmailReplyBinding + " root-1":  "not_a_reply",
		gmailReplyBinding + " other-1": "not_a_reply",
	}, ignored)
	require.NotContains(t, logs.Text(), "SENTINEL")
	require.NotContains(t, logs.Text(), "@example.com", "records never contain a sender address")
}

// sentinelAccessToken stands in for a Gmail access token. No log record may contain it.
const sentinelAccessToken = "SENTINEL-ACCESS-TOKEN"

type gmailSentinelMessage struct {
	id       string
	threadID string
	reply    bool
	from     string
}

// newGmailSentinelProvider fails its first list request, then serves messages newest first. Every subject,
// snippet, and body contains a sentinel that must never reach a log record.
func newGmailSentinelProvider(t *testing.T, messages []gmailSentinelMessage) *httptest.Server {
	t.Helper()
	var lists atomic.Int32
	byID := make(map[string]gmailSentinelMessage, len(messages))
	references := make([]map[string]string, 0, len(messages))
	for _, message := range messages {
		byID[message.id] = message
		references = append(references, map[string]string{"id": message.id, "threadId": message.threadID})
	}
	listing, err := json.Marshal(map[string]any{"messages": references})
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+sentinelAccessToken {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		if request.URL.Path == "/users/me/messages" {
			if lists.Add(1) == 1 {
				http.Error(response, "SENTINEL-PROVIDER-ERROR-BODY", http.StatusInternalServerError)
				return
			}
			_, _ = response.Write(listing)
			return
		}
		message, ok := byID[strings.TrimPrefix(request.URL.Path, "/users/me/messages/")]
		if !ok {
			http.NotFound(response, request)
			return
		}
		headers := []map[string]string{
			{"name": "Message-ID", "value": "<" + message.id + "@example.com>"},
			{"name": "From", "value": message.from},
			{"name": "To", "value": "owner@example.com"},
			{"name": "Subject", "value": "SENTINEL-SUBJECT approval request"},
		}
		if message.reply {
			headers = append(headers,
				map[string]string{"name": "In-Reply-To", "value": "<root-1@example.com>"},
				map[string]string{"name": "References", "value": "<root-1@example.com>"},
			)
		}
		contents, err := json.Marshal(map[string]any{
			"id": message.id, "threadId": message.threadID, "labelIds": []string{"INBOX"},
			"snippet": "SENTINEL-SNIPPET approve", "internalDate": "1000",
			"payload": map[string]any{"mimeType": "multipart/alternative", "headers": headers, "parts": []map[string]any{{
				"mimeType": "text/plain", "body": map[string]string{"data": base64.RawURLEncoding.EncodeToString([]byte("SENTINEL-BODY approve"))},
			}}},
		})
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = response.Write(contents)
	}))
	t.Cleanup(server.Close)
	return server
}

// newGmailArrivalProvider serves an empty inbox until it has answered the first list request whose search
// query is arriveAfter. Every later list request returns messages, newest first.
func newGmailArrivalProvider(t *testing.T, arriveAfter string, messages ...gmailPageMessage) *httptest.Server {
	t.Helper()
	var mutex sync.Mutex
	arrived := false
	byID := make(map[string]gmailPageMessage, len(messages))
	references := make([]map[string]string, 0, len(messages))
	for _, message := range messages {
		byID[message.id] = message
		references = append(references, map[string]string{"id": message.id, "threadId": message.threadID})
	}
	listing, err := json.Marshal(map[string]any{"messages": references})
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/users/me/messages" {
			mutex.Lock()
			defer mutex.Unlock()
			if !arrived {
				arrived = request.URL.Query().Get("q") == arriveAfter
				_, _ = response.Write([]byte(`{"messages":[]}`))
				return
			}
			_, _ = response.Write(listing)
			return
		}
		mutex.Lock()
		message, ok := byID[strings.TrimPrefix(request.URL.Path, "/users/me/messages/")]
		visible := arrived
		mutex.Unlock()
		if !ok || !visible {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write([]byte(gmailMessageJSON(message.id, message.threadID, message.reply, "", "")))
	}))
	t.Cleanup(server.Close)
	return server
}

// orderedDeliveries records root and reply deliveries in the order the runner makes them.
type orderedDeliveries struct {
	mutex  sync.Mutex
	events []string
}

func (deliveries *orderedDeliveries) record(kind string, eventID string) {
	deliveries.mutex.Lock()
	defer deliveries.mutex.Unlock()
	deliveries.events = append(deliveries.events, kind+":"+eventID)
}

func (deliveries *orderedDeliveries) snapshot() []string {
	deliveries.mutex.Lock()
	defer deliveries.mutex.Unlock()
	return append([]string(nil), deliveries.events...)
}

// localConfig routes the stored root and reply bindings to recording targets. rootFailure, when set, fails every root delivery.
func (deliveries *orderedDeliveries) localConfig(rootFailure func() error) gmail.LocalMessageTriggerRunnerConfig {
	return gmail.LocalMessageTriggerRunnerConfig{
		MessageReceivedRoutes: []gmail.LocalMessageReceivedTriggerRoute{{
			BindingName: gmailRootBinding,
			Target: sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[gmail.MessageEvent]) error {
				if rootFailure != nil {
					return rootFailure()
				}
				deliveries.record("root", event.ID)
				return nil
			}),
		}},
		ReplyReceivedRoutes: []gmail.LocalReplyReceivedTriggerRoute{{
			BindingName: gmailReplyBinding,
			Target: sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[gmail.MessageEvent]) error {
				deliveries.record("reply", event.ID)
				return nil
			}),
		}},
	}
}

type gmailPageMessage struct {
	id       string
	threadID string
	reply    bool
}

func (message gmailPageMessage) event() sdkgo.TriggerEvent[gmail.MessageEvent] {
	return sdkgo.TriggerEvent[gmail.MessageEvent]{ID: message.id, OccurredAt: time.Unix(1, 0).UTC(), Payload: gmail.MessageEvent{
		PrimaryEmail: "owner@example.com", MessageID: message.id, ThreadID: message.threadID, IsReply: message.reply,
	}}
}

// gmailPageProvider serves one fixed inbox page and counts list requests by search query.
type gmailPageProvider struct {
	*httptest.Server
	mutex      sync.Mutex
	listByTerm map[string]int32
}

func newGmailPageProvider(t *testing.T, page []gmailPageMessage) *gmailPageProvider {
	t.Helper()
	provider := &gmailPageProvider{listByTerm: map[string]int32{}}
	messages := make(map[string]gmailPageMessage, len(page))
	references := make([]map[string]string, 0, len(page))
	for _, message := range page {
		messages[message.id] = message
		references = append(references, map[string]string{"id": message.id, "threadId": message.threadID})
	}
	listing, err := json.Marshal(map[string]any{"messages": references})
	require.NoError(t, err)
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/users/me/messages" {
			provider.mutex.Lock()
			provider.listByTerm[request.URL.Query().Get("q")]++
			provider.mutex.Unlock()
			_, _ = response.Write(listing)
			return
		}
		message, ok := messages[strings.TrimPrefix(request.URL.Path, "/users/me/messages/")]
		if !ok {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write([]byte(gmailMessageJSON(message.id, message.threadID, message.reply, "", "")))
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *gmailPageProvider) listCount(searchQuery string) int32 {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.listByTerm[searchQuery]
}

func newGmailLocalStore(t *testing.T, endpoint string) (*localconfig.Store, string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "connections.json")
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": gmail.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/google/gmail",
			"moduleVersion": "v0.8.0", "provider": "google", "connectionName": gmailLocalConnection,
			"configuration": map[string]any{"endpoint": endpoint, "pollInterval": int64(time.Second)},
			"credentials":   map[string]any{"access_token": "gmail-token", "primary_email": "owner@example.com"},
		}},
		"triggerBindings": []any{
			map[string]any{"connectorId": gmail.ConnectorID, "connectionName": gmailLocalConnection, "triggerName": "messageReceived",
				"bindingName": gmailRootBinding, "configuration": map[string]any{"searchQuery": "root-query"}},
			map[string]any{"connectorId": gmail.ConnectorID, "connectionName": gmailLocalConnection, "triggerName": "replyReceived",
				"bindingName": gmailReplyBinding, "configuration": map[string]any{"searchQuery": "reply-query"}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	return store, directory
}

// pendingGmailEventIDs lists the event IDs persisted in every Trigger inbox in directory.
func pendingGmailEventIDs(t *testing.T, directory string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(directory, ".trigger-inbox-*.json"))
	require.NoError(t, err)
	eventIDs := []string{}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		var inbox struct {
			Events []struct {
				EventID string `json:"eventId"`
			} `json:"events"`
		}
		require.NoError(t, json.Unmarshal(contents, &inbox))
		for _, event := range inbox.Events {
			eventIDs = append(eventIDs, event.EventID)
		}
	}
	return eventIDs
}

// runInBackground starts run and returns a function that cancels it and waits for context.Canceled.
func runInBackground(t *testing.T, run func(context.Context) error) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- run(ctx) }()
	t.Cleanup(cancel)
	return func() {
		t.Helper()
		cancel()
		select {
		case err := <-result:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(4 * time.Second):
			t.Fatal("Gmail Trigger runner did not stop")
		}
	}
}

func receiveEventIDs(t *testing.T, eventIDs <-chan string, count int) []string {
	t.Helper()
	received := make([]string, 0, count)
	for range count {
		select {
		case eventID := <-eventIDs:
			received = append(received, eventID)
		case <-time.After(4 * time.Second):
			t.Fatalf("received %v, want %d Gmail Trigger events", received, count)
		}
	}
	return received
}
