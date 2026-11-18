// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validCreateTicketInput() gorgias.CreateTicketInput {
	return gorgias.CreateTicketInput{
		Subject: "Double charge on order 88213", Description: "I was charged twice <order 88213>.\nPlease help.",
		Requester: gorgias.TicketRequesterInput{Email: "jane@acme.example.com", Name: "Jane Smith"},
		Status:    gorgias.TicketStatusOpen, Priority: gorgias.TicketPriorityHigh, Tags: []string{"billing"}, AssigneeTeamID: 8,
	}
}

func externalIDOf(t *testing.T, body string) string {
	t.Helper()
	var payload struct {
		ExternalID string `json:"external_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &payload))
	return payload.ExternalID
}

// emptyTicketPage is a lookup that finds no ticket with the external ID.
const emptyTicketPage = `{"data":[],"meta":{"next_cursor":null}}`

func requireRetryAfter(t *testing.T, err error) *dex.RetryAfterError {
	t.Helper()
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	return retryAfter
}

// requireRetry asserts a Retry with the SDK's default delay and returns its Failure.
func requireRetry(t *testing.T, err error) sdkgo.Failure {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	return retry.Failure
}

func TestCreateTicketSendsTheFirstMessageThroughTheAPIChannelWithTheIdempotencyKey(t *testing.T) {
	var provider *recordingGorgias
	provider = newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		ticket := ticketJSON(5512, "open", []string{"billing"}, nil)
		ticket["external_id"] = externalIDOf(t, provider.request(index).body)
		writeValue(t, response, http.StatusCreated, ticket)
	})
	ctx := newGorgiasDexContext("create")
	result, err := sdkgo.RunMutation(ctx, newGorgiasClient(t, provider.URL).CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchCreated, result.Branch)
	require.False(t, result.Value.WasCreatedByEarlierAttempt)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/api/tickets", request.path)
	key := string(result.Receipt.IdempotencyKey)
	require.True(t, strings.HasPrefix(key, "dex-"))
	require.JSONEq(t, `{"customer":{"email":"jane@acme.example.com","name":"Jane Smith"},"subject":"Double charge on order 88213",
		"channel":"api","via":"api","from_agent":false,"status":"open","priority":"high","tags":[{"name":"billing"}],"assignee_team":{"id":8},
		"external_id":"`+key+`","messages":[{"channel":"api","via":"api","from_agent":false,"sender":{"email":"jane@acme.example.com","name":"Jane Smith"},
		"subject":"Double charge on order 88213","body_text":"I was charged twice <order 88213>.\nPlease help.",
		"body_html":"I was charged twice &lt;order 88213&gt;.<br>Please help.","stripped_text":"I was charged twice <order 88213>.\nPlease help."}]}`, request.body)
	require.Equal(t, key, result.Value.Ticket.ExternalID)
	require.Equal(t, "5512", result.Receipt.ProviderObjectID)
	require.Contains(t, string(ctx.recordedHeartbeat), `"providerId":"5512"`, "the fence records the created ticket ID")
}

func TestCreateTicketWithALostResponseFindsTheTicketByExternalIDWithoutResending(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			dropConnection(t, response)
			return
		}
		ticket := ticketJSON(5512, "open", nil, nil)
		delete(ticket, "messages")
		ticket["external_id"] = request.URL.Query().Get("external_id")
		other := ticketJSON(9, "open", nil, nil)
		delete(other, "messages")
		writeValue(t, response, http.StatusOK, ticketPage(nil, other, ticket))
	})
	client := newGorgiasClient(t, provider.URL)
	first := newGorgiasDexContext("lost-create")
	_, err := sdkgo.RunMutation(first, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.Equal(t, 2*time.Second, requireRetryAfter(t, err).After, "an unconfirmed create is retried, and the retry looks the ticket up first")

	result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchCreated, result.Branch)
	require.True(t, result.Value.WasCreatedByEarlierAttempt)
	require.Equal(t, int64(5512), result.Value.Ticket.ID)
	require.Equal(t, 2, provider.requestCount())
	lookup := provider.request(1)
	require.Equal(t, http.MethodGet, lookup.method)
	require.Equal(t, externalIDOf(t, provider.request(0).body), lookup.query["external_id"][0])
}

func TestCreateTicketResendsAnUnknownOutcomeBelowTheSendCapAfterAnEmptyLookup(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case index == 0:
			writeJSON(t, response, http.StatusBadGateway, `{"error":{"msg":"SENTINEL"}}`)
		case request.Method == http.MethodGet:
			writeJSON(t, response, http.StatusOK, emptyTicketPage)
		default:
			writeValue(t, response, http.StatusCreated, ticketJSON(5512, "open", nil, nil))
		}
	})
	client := newGorgiasClient(t, provider.URL)
	first := newGorgiasDexContext("resend")
	_, err := sdkgo.RunMutation(first, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.Equal(t, 2*time.Second, requireRetryAfter(t, err).After)

	result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchCreated, result.Branch)
	require.False(t, result.Value.WasCreatedByEarlierAttempt)
	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method, "the retry looks for the ticket before it resends")
	require.Equal(t, http.MethodPost, provider.request(2).method)
	require.Equal(t, externalIDOf(t, provider.request(0).body), externalIDOf(t, provider.request(2).body), "the resend keeps the external ID")
}

func TestCreateTicketReadBackAtTheSendCap(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		body        string
		retryAfter  string
		retryDelay  time.Duration
		isRetry     bool
		contains    string
		isFoundOnce bool
	}{
		{name: "no ticket", status: http.StatusOK, body: emptyTicketPage, contains: "shows no ticket"},
		{name: "rejected lookup", status: http.StatusForbidden, body: `{"error":{"msg":"SENTINEL"}}`, contains: "could not confirm"},
		{name: "invalid lookup", status: http.StatusOK, body: `{"meta":{}}`, contains: "could not confirm"},
		{name: "lookup outage", status: http.StatusBadGateway, body: `{"error":{"msg":"SENTINEL"}}`, retryAfter: "30", retryDelay: 30 * time.Second, isRetry: true},
		{name: "rate limited lookup", status: http.StatusTooManyRequests, body: `{"error":{"msg":"SENTINEL"}}`, retryAfter: "600", retryDelay: time.Minute, isRetry: true},
		{name: "found ticket", isFoundOnce: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var postCount atomic.Int32
			provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				switch {
				case request.Method == http.MethodPost:
					postCount.Add(1)
					writeJSON(t, response, http.StatusBadGateway, `{"error":{"msg":"SENTINEL"}}`)
				case postCount.Load() < 3:
					writeJSON(t, response, http.StatusOK, emptyTicketPage)
				case test.isFoundOnce:
					ticket := ticketJSON(5512, "open", nil, nil)
					delete(ticket, "messages")
					ticket["external_id"] = request.URL.Query().Get("external_id")
					writeValue(t, response, http.StatusOK, ticketPage(nil, ticket))
				default:
					if test.retryAfter != "" {
						response.Header().Set("Retry-After", test.retryAfter)
					}
					writeJSON(t, response, test.status, test.body)
				}
			})
			client := newGorgiasClient(t, provider.URL)
			ctx := newGorgiasDexContext("cap-" + test.name)
			for range 3 {
				_, err := sdkgo.RunMutation(ctx, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
				requireRetryAfter(t, err)
				ctx = ctx.nextAttempt()
			}
			require.Equal(t, int32(3), postCount.Load(), "the manifest send cap of three allows three sends")
			result, err := sdkgo.RunMutation(ctx, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
			require.Equal(t, int32(3), postCount.Load(), "an attempt at the cap never sends")
			if test.isRetry {
				require.Equal(t, test.retryDelay, requireRetryAfter(t, err).After, "a lookup Gorgias did not answer is repeated")
				return
			}
			require.NoError(t, err)
			if test.isFoundOnce {
				require.Equal(t, gorgias.CreateTicketBranchCreated, result.Branch)
				require.True(t, result.Value.WasCreatedByEarlierAttempt)
				return
			}
			require.Equal(t, gorgias.CreateTicketBranchUncertain, result.Branch)
			require.Contains(t, result.Failure.Message, test.contains)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
		})
	}
}

func TestCreateTicketCredentialFailureAfterAnEarlierSendRetriesOrSelectsUncertain(t *testing.T) {
	for _, test := range credentialFailureCases {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
			first := newGorgiasDexContext("credentials-" + test.name)
			_, err := sdkgo.RunMutation(first, newGorgiasClient(t, provider.URL).CreateTicket(), gorgiasConnection, validCreateTicketInput())
			requireRetryAfter(t, err)

			second := first.nextAttempt()
			result, err := sdkgo.RunMutation(second, newGorgiasClientWithCredentials(t, provider.URL, test.credentials).CreateTicket(), gorgiasConnection, validCreateTicketInput())
			require.Equal(t, 1, provider.requestCount())
			require.Empty(t, second.recordedHeartbeat, "notSent would hide the earlier possible send")
			if !test.isTerminal {
				require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind, "credentials that could not be read this time are retried")
				return
			}
			require.NoError(t, err)
			require.Equal(t, gorgias.CreateTicketBranchUncertain, result.Branch, "a possibly sent ticket is never reported as a defect")
			require.Contains(t, result.Failure.Message, "credentials are unusable")
		})
	}
}

func TestCreateTicketCredentialFailureOnAFirstSendRecordsNotSent(t *testing.T) {
	for _, test := range credentialFailureCases {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingGorgias(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
			ctx := newGorgiasDexContext("first-credentials-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newGorgiasClientWithCredentials(t, provider.URL, test.credentials).CreateTicket(), gorgiasConnection, validCreateTicketInput())
			require.Contains(t, string(ctx.recordedHeartbeat), `"state":"notSent"`)
			if !test.isTerminal {
				requireRetry(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, gorgias.CreateTicketBranchDefect, result.Branch)
		})
	}
}

func TestCreateTicketRetriesAnUnknownOutcomeAfterRetryAfterWithTheClaimKept(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "20")
		writeJSON(t, response, http.StatusInternalServerError, `{"error":{"msg":"SENTINEL"}}`)
	})
	ctx := newGorgiasDexContext("unavailable")
	_, err := sdkgo.RunMutation(ctx, newGorgiasClient(t, provider.URL).CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.Equal(t, 20*time.Second, requireRetryAfter(t, err).After)
	require.Contains(t, string(ctx.recordedHeartbeat), `"state":"claimed"`, "the claim stays, so the retry looks the ticket up first")
}

func TestCreateTicketResendsWhatGorgiasProvablyDidNotProcessWithoutALookup(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, index int) {
			if index == 0 {
				response.Header().Set("Retry-After", "3")
				writeJSON(t, response, status, `{"error":{"msg":"SENTINEL"}}`)
				return
			}
			writeValue(t, response, http.StatusCreated, ticketJSON(5512, "open", nil, nil))
		})
		client := newGorgiasClient(t, provider.URL)
		first := newGorgiasDexContext("not-processed")
		_, err := sdkgo.RunMutation(first, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
		require.Equal(t, 3*time.Second, requireRetryAfter(t, err).After, "HTTP %d", status)
		require.Contains(t, string(first.recordedHeartbeat), `"state":"canResend"`)
		result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
		require.NoError(t, err)
		require.Equal(t, gorgias.CreateTicketBranchCreated, result.Branch)
		require.Equal(t, 2, provider.requestCount())
		require.Equal(t, http.MethodPost, provider.request(1).method, "nothing can be found, so the retry sends without a lookup")
	}
}

func TestCreateTicketLaterAttemptWithoutAMarkerFindsTheTicketInsteadOfResending(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		ticket := ticketJSON(5512, "open", nil, nil)
		delete(ticket, "messages")
		ticket["external_id"] = request.URL.Query().Get("external_id")
		writeValue(t, response, http.StatusOK, ticketPage(nil, ticket))
	})
	unmarked := newGorgiasDexContext("unstored-marker").nextAttempt()
	result, err := sdkgo.RunMutation(unmarked, newGorgiasClient(t, provider.URL).CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchCreated, result.Branch)
	require.True(t, result.Value.WasCreatedByEarlierAttempt)
	require.Equal(t, 1, provider.requestCount(), "the ticket an earlier attempt sent before Dex stored its marker is not created again")
	require.Equal(t, http.MethodGet, provider.request(0).method)
}

func TestCreateTicketWithASavedTicketIDReadsBackByIDAndNeverResends(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		isOurs bool
		branch sdkgo.BranchID
	}{
		{name: "found", status: http.StatusOK, isOurs: true, branch: gorgias.CreateTicketBranchCreated},
		{name: "another ticket", status: http.StatusOK, branch: gorgias.CreateTicketBranchUncertain},
		{name: "missing", status: http.StatusNotFound, branch: gorgias.CreateTicketBranchUncertain},
		{name: "outage", status: http.StatusBadGateway},
	} {
		t.Run(test.name, func(t *testing.T) {
			var externalID string
			provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPost {
					writeValue(t, response, http.StatusCreated, ticketJSON(5512, "open", nil, nil))
					return
				}
				ticket := ticketJSON(5512, "open", nil, nil)
				if test.isOurs {
					ticket["external_id"] = externalID
				}
				writeValue(t, response, test.status, ticket)
			})
			client := newGorgiasClient(t, provider.URL)
			first := newGorgiasDexContext("saved-id-" + test.name)
			_, err := sdkgo.RunMutation(first, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
			require.NoError(t, err)
			externalID = externalIDOf(t, provider.request(0).body)

			result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
			require.Equal(t, 2, provider.requestCount(), "Gorgias accepted the earlier send, so it is never resent")
			require.Equal(t, http.MethodGet, provider.request(1).method)
			require.Equal(t, "/api/tickets/5512", provider.request(1).path)
			if test.branch == "" {
				requireRetryAfter(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.isOurs {
				require.True(t, result.Value.WasCreatedByEarlierAttempt)
			}
		})
	}
}

func TestCreateTicketAcceptedWithAnUnusableAnswerIsReadBackAndNeverResent(t *testing.T) {
	for _, test := range []struct {
		name             string
		reply            func(http.ResponseWriter)
		isFound          bool
		isReadBackByID   bool
		maxResponseBytes int64
	}{
		{name: "body breaks after 2xx headers, ticket found", reply: func(response http.ResponseWriter) { breakBodyAfterHeaders(t, response, http.StatusCreated) }, isFound: true},
		{name: "body breaks after 2xx headers, nothing found", reply: func(response http.ResponseWriter) { breakBodyAfterHeaders(t, response, http.StatusCreated) }},
		{name: "undecodable 2xx naming the ticket", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"id":5512,"status":7}`)
		}, isFound: true, isReadBackByID: true},
		{name: "undecodable 2xx naming no ticket", reply: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusCreated, `not json`) }},
		{name: "oversized 2xx", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"id":5512,"padding":"`+strings.Repeat("x", 4096)+`"}`)
		}, isFound: true, maxResponseBytes: 2048},
	} {
		t.Run(test.name, func(t *testing.T) {
			var postCount atomic.Int32
			var provider *recordingGorgias
			provider = newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				switch {
				case request.Method == http.MethodPost:
					postCount.Add(1)
					test.reply(response)
				case !test.isFound:
					writeJSON(t, response, http.StatusOK, emptyTicketPage)
				default:
					ticket := ticketJSON(5512, "open", nil, nil)
					delete(ticket, "messages")
					ticket["external_id"] = externalIDOf(t, provider.request(0).body)
					if test.isReadBackByID {
						writeValue(t, response, http.StatusOK, ticket)
						return
					}
					writeValue(t, response, http.StatusOK, ticketPage(nil, ticket))
				}
			})
			config := gorgias.Config{Domain: testDomain, MaxResponseBytes: test.maxResponseBytes}
			client, err := gorgias.New(config, testCredentialProvider(), gorgias.WithAPIBaseURL(provider.URL+"/api"))
			require.NoError(t, err)
			ctx := newGorgiasDexContext("accepted")
			result, err := sdkgo.RunMutation(ctx, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
			require.NoError(t, err, "an accepted create is never retried without a saved ticket ID")
			require.Equal(t, int32(1), postCount.Load())
			require.Equal(t, 2, provider.requestCount())
			if test.isReadBackByID {
				require.Equal(t, "/api/tickets/5512", provider.request(1).path)
				require.Contains(t, string(ctx.recordedHeartbeat), `"providerId":"5512"`)
			} else {
				require.Equal(t, externalIDOf(t, provider.request(0).body), provider.request(1).query["external_id"][0])
			}
			if !test.isFound {
				require.Equal(t, gorgias.CreateTicketBranchUncertain, result.Branch)
				return
			}
			require.Equal(t, gorgias.CreateTicketBranchCreated, result.Branch)
			require.Equal(t, int64(5512), result.Value.Ticket.ID)
		})
	}
}

func TestCreateTicketRefusalWithAnOversizedBodyIsAnUnknownOutcome(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, `{"error":{"msg":"`+strings.Repeat("x", 4096)+`"}}`)
	})
	client, err := gorgias.New(gorgias.Config{Domain: testDomain, MaxResponseBytes: 2048}, testCredentialProvider(), gorgias.WithAPIBaseURL(provider.URL+"/api"))
	require.NoError(t, err)
	ctx := newGorgiasDexContext("oversized-refusal")
	_, err = sdkgo.RunMutation(ctx, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.Equal(t, 2*time.Second, requireRetryAfter(t, err).After, "an unread refusal body cannot prove nothing was applied")
	require.Contains(t, string(ctx.recordedHeartbeat), `"state":"claimed"`, "the send counts, so the retry reads back first")
}

func TestCreateTicketWithAPreviousReleaseMarkerRetriesWithoutSending(t *testing.T) {
	provider := newRecordingGorgias(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	ctx := newPreviousReleaseMarkerContext("previous-release", 2)
	_, err := sdkgo.RunMutation(ctx, newGorgiasClient(t, provider.URL).CreateTicket(), gorgiasConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "the SDK fence fails closed on the untrusted marker and returns Retry")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Zero(t, provider.requestCount())
}

func TestCreateTicketRefusalAfterAnUnknownOutcomeSelectsUncertain(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case index == 0:
			writeJSON(t, response, http.StatusBadGateway, `{"error":{"msg":"SENTINEL"}}`)
		case request.Method == http.MethodGet:
			writeJSON(t, response, http.StatusOK, emptyTicketPage)
		default:
			writeJSON(t, response, http.StatusBadRequest, `{"error":{"msg":"SENTINEL"}}`)
		}
	})
	client := newGorgiasClient(t, provider.URL)
	first := newGorgiasDexContext("refusal-after-unknown")
	_, err := sdkgo.RunMutation(first, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	requireRetryAfter(t, err)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchUncertain, result.Branch, "the first send may have created the ticket")
}

func TestCreateTicketRejectionsAndDefects(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, `{"error":{"msg":"SENTINEL","data":{"assignee_team":["SENTINEL unknown"]}}}`)
	})
	client := newGorgiasClient(t, provider.URL)
	rejected := newGorgiasDexContext("rejected")
	result, err := sdkgo.RunMutation(rejected, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchProviderRejected, result.Branch)
	require.Equal(t, "Gorgias rejected the request (HTTP 400) [fields: assignee_team]", result.Failure.Message)
	require.Contains(t, string(rejected.recordedHeartbeat), `"state":"canResend"`, "a definite refusal records that nothing was applied")

	rejecting := newGorgiasDexContext("heartbeat")
	rejecting.rejectsHeartbeat = true
	_, err = sdkgo.RunMutation(rejecting, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.Error(t, err, "nothing is sent until Dex records the checkpoint")
	require.Equal(t, 1, provider.requestCount())

	for name, change := range map[string]func(*gorgias.CreateTicketInput){
		"blank subject": func(input *gorgias.CreateTicketInput) { input.Subject = " " },
		"long subject": func(input *gorgias.CreateTicketInput) {
			input.Subject = strings.Repeat("s", gorgias.MaxSubjectLength+1)
		},
		"display requester": func(input *gorgias.CreateTicketInput) { input.Requester.Email = "Jane <jane@example.com>" },
		"zendesk status":    func(input *gorgias.CreateTicketInput) { input.Status = "pending" },
		"zendesk priority":  func(input *gorgias.CreateTicketInput) { input.Priority = "urgent" },
		"duplicate tag":     func(input *gorgias.CreateTicketInput) { input.Tags = []string{"a", "a"} },
	} {
		input := validCreateTicketInput()
		change(&input)
		result, err := sdkgo.RunMutation(newGorgiasDexContext("invalid"), client.CreateTicket(), gorgiasConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, gorgias.CreateTicketBranchDefect, result.Branch, name)
	}
	require.Equal(t, 1, provider.requestCount())
}
