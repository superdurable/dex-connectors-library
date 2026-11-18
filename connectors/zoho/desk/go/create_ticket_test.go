// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validCreateTicketInput() desk.CreateTicketInput {
	return desk.CreateTicketInput{
		Subject: "Double charge on order 88213", Description: "I was charged <b>twice</b>.\nOrder 88213 & 88214.",
		DepartmentID: testDepartmentID,
		Contact:      desk.TicketContactInput{Email: "jane@acme.example.com", FirstName: "Jane", LastName: "Smith"},
		Status:       desk.TicketStatusOpen, Priority: desk.TicketPriorityHigh, AssigneeID: testAgentID,
	}
}

func TestCreateTicketSendsEscapedHTMLAndZohoDeskNamesOnce(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099001", "Open", "Open"))
	})
	ctx := newDeskDexContext("create")
	result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, "1892000000099001", result.Value.Ticket.ID)
	require.Equal(t, "1892000000099001", result.Receipt.ProviderObjectID)
	require.Equal(t, 1, provider.requestCount())
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/api/v1/tickets", request.path)
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.Empty(t, request.header.Get("Idempotency-Key"), "Zoho Desk documents no key, and Go would retry a keyed POST on its own")
	require.JSONEq(t, `{"subject":"Double charge on order 88213","description":"I was charged &lt;b&gt;twice&lt;/b&gt;.<br>Order 88213 &amp; 88214.",
		"departmentId":"`+testDepartmentID+`","contact":{"email":"jane@acme.example.com","firstName":"Jane","lastName":"Smith"},
		"email":"jane@acme.example.com","status":"Open","priority":"High","assigneeId":"`+testAgentID+`"}`, request.body)
	marker := recordedMarker(t, ctx)
	require.Equal(t, result.Receipt.CallID, marker.CallID)
	require.Equal(t, singledispatch.StateClaimed, marker.State, "the fence claim is recorded before the request")
	require.Equal(t, "1892000000099001", marker.ProviderID, "the accepted ticket is checkpointed and never resent")
}

func TestCreateTicketForAnExistingContactOmitsUnsetFields(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099002", "Open", "Open"))
	})
	_, err := sdkgo.RunMutation(newDeskDexContext("create-contact-id"), newDeskClient(t, provider.URL).CreateTicket(), deskConnection, desk.CreateTicketInput{
		Subject: "Password reset", Description: "Please reset it.", DepartmentID: testDepartmentID, ContactID: testContactID,
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"subject":"Password reset","description":"Please reset it.","departmentId":"`+testDepartmentID+`","contactId":"`+testContactID+`"}`,
		provider.request(0).body)
}

func TestCreateTicketResendsAnUnknownOutcomeBelowTheSendCap(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
		kind  sdkgo.FailureKind
	}{
		{name: "server error", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"errorCode":"INTERNAL_SERVER_ERROR","message":"SENTINEL"}`)
		}, kind: sdkgo.FailureAvailability},
		{name: "gateway timeout", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusGatewayTimeout, ``)
		}, kind: sdkgo.FailureAvailability},
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) }, kind: sdkgo.FailureTransport},
		{name: "conflict", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusConflict, `{"errorCode":"CONFLICT","message":"SENTINEL"}`)
		}, kind: sdkgo.FailureConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					test.reply(response)
					return
				}
				writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099001", "Open", "Open"))
			})
			client := newDeskClient(t, provider.URL)
			ctx := newDeskDexContext("create-" + test.name)
			_, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
			retry := requireRetry(t, err, test.kind)
			require.NotContains(t, retry.Failure.Message, testAccessToken)
			marker := recordedMarker(t, ctx)
			require.Equal(t, singledispatch.StateClaimed, marker.State, "the claim stays, so the next attempt counts this send")
			require.Equal(t, int32(1), marker.SendCount)

			result, err := sdkgo.RunMutation(ctx.nextAttempt(), client.CreateTicket(), deskConnection, validCreateTicketInput())
			require.NoError(t, err)
			require.Equal(t, desk.CreateTicketBranchCreated, result.Branch, "one resend is below the send cap of three")
			require.Equal(t, 2, provider.requestCount())
		})
	}
}

func TestCreateTicketSelectsUncertainAtTheSendCap(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusInternalServerError, `{"errorCode":"INTERNAL_SERVER_ERROR","message":"SENTINEL"}`)
	})
	client := newDeskClient(t, provider.URL)
	ctx := newDeskDexContext("create-cap")
	_, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx = ctx.nextAttempt()
	_, err = sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx = ctx.nextAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchUncertain, result.Branch, "the third possible send reaches the cap")
	requireNoSentinel(t, result)
	require.Equal(t, 3, provider.requestCount())

	result, err = sdkgo.RunMutation(ctx.nextAttempt(), client.CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchUncertain, result.Branch)
	require.Equal(t, "earlier attempts may have sent the request and the send cap is reached, so it is not sent again", result.Failure.Message)
	require.Equal(t, 3, provider.requestCount(), "nothing is sent at the cap")
}

func TestCreateTicketNeverResendsADefiniteRefusal(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
		kind  sdkgo.FailureKind
	}{
		{name: "invalid data", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusUnprocessableEntity, `{"errorCode":"INVALID_DATA","message":"SENTINEL","errors":[{"fieldName":"/departmentId","errorType":"invalid"}]}`)
		}, kind: sdkgo.FailureValidation},
		{name: "scope mismatch", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusForbidden, `{"errorCode":"SCOPE_MISMATCH"}`)
		}, kind: sdkgo.FailureAuthorization},
		{name: "unauthorized without refresh", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusUnauthorized, `{"errorCode":"INVALID_OAUTH","message":"SENTINEL"}`)
		}, kind: sdkgo.FailureAuthentication},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
			ctx := newDeskDexContext("create-refused-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
			require.NoError(t, err)
			require.Equal(t, desk.CreateTicketBranchProviderRejected, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			requireNoSentinel(t, result)
			require.Equal(t, singledispatch.StateCanResend, recordedMarker(t, ctx).State, "canResend is recorded before the refusal branch")
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestCreateTicketRefusalAfterAnEarlierPossibleSendIsUncertain(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			dropConnection(t, response)
			return
		}
		writeJSON(t, response, http.StatusUnprocessableEntity, `{"errorCode":"INVALID_DATA"}`)
	})
	client := newDeskClient(t, provider.URL)
	ctx := newDeskDexContext("create-refused-after-unknown")
	_, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	result, err := sdkgo.RunMutation(ctx.nextAttempt(), client.CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchUncertain, result.Branch, "the first send may have created the ticket")
	require.Equal(t, 2, provider.requestCount())
}

// TestCreateTicketReadsBackAnAcceptedTicketByID covers a 2xx whose ticket does not decode but
// carries its ID: it is read back by ID in the same attempt and never resent.
func TestCreateTicketReadsBackAnAcceptedTicketByID(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusOK, `{"id":"1892000000099004","subject":"SENTINEL"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099004", "Open", "Open"))
	})
	ctx := newDeskDexContext("create-accepted-invalid")
	result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchCreated, result.Branch, "a returned ticket ID proves the ticket exists")
	require.Equal(t, "1892000000099004", result.Value.Ticket.ID)
	require.Equal(t, "1892000000099004", result.Receipt.ProviderObjectID)
	require.Equal(t, "1892000000099004", recordedMarker(t, ctx).ProviderID)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
	require.Equal(t, "/api/v1/tickets/1892000000099004", provider.request(1).path)
}

// TestCreateTicketReadsBackASavedTicketIDOnLaterAttempts covers a read-back that fails transiently
// after the ID was checkpointed: later attempts read back again and never resend.
func TestCreateTicketReadsBackASavedTicketIDOnLaterAttempts(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case request.Method == http.MethodPost:
			writeJSON(t, response, http.StatusOK, `{"id":"1892000000099004","subject":"SENTINEL"}`)
		case index == 1:
			writeJSON(t, response, http.StatusInternalServerError, `{"errorCode":"INTERNAL_SERVER_ERROR"}`)
		case index == 2:
			response.Header().Set("Retry-After", "3")
			writeJSON(t, response, http.StatusTooManyRequests, `{"errorCode":"TOO_MANY_REQUESTS"}`)
		default:
			writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099004", "Open", "Open"))
		}
	})
	client := newDeskClient(t, provider.URL)
	ctx := newDeskDexContext("create-read-back-later")
	_, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, "1892000000099004", recordedMarker(t, ctx).ProviderID)

	ctx = ctx.nextAttempt()
	_, err = sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	require.Zero(t, ctx.heartbeatCount, "a read-back records nothing, so the saved ID stays")

	result, err := sdkgo.RunMutation(ctx.nextAttempt(), client.CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, "1892000000099004", result.Value.Ticket.ID)
	require.Equal(t, 4, provider.requestCount())
	for index := 1; index < 4; index++ {
		require.Equal(t, http.MethodGet, provider.request(index).method, "a saved ticket ID is never resent")
	}
}

// TestCreateTicketReadsBackASavedIDAtTheSendCap covers finding 39: a saved ID is read back even when
// the send cap is reached, and only an unconfirmed read-back selects uncertain.
func TestCreateTicketReadsBackASavedIDAtTheSendCap(t *testing.T) {
	for _, test := range []struct {
		name    string
		reply   func(http.ResponseWriter)
		branch  sdkgo.BranchID
		isRetry bool
	}{
		{name: "found", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099004", "Open", "Open"))
		}, branch: desk.CreateTicketBranchCreated},
		{name: "unavailable", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadGateway, ``)
		}, isRetry: true},
		{name: "not found", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND"}`)
		}, branch: desk.CreateTicketBranchUncertain},
		{name: "forbidden", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusForbidden, `{"errorCode":"SCOPE_MISMATCH"}`)
		}, branch: desk.CreateTicketBranchUncertain},
		{name: "another ticket", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099005", "Open", "Open"))
		}, branch: desk.CreateTicketBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := capClaimWithSavedProviderID(t, "create-saved-id-"+test.name, func(client *desk.Client, ctx *deskDexContext) {
				_, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
				require.NoError(t, err)
			}, ticketBodyJSON("1892000000099004", "Open", "Open"))
			provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
			result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
			require.Equal(t, 1, provider.requestCount())
			require.Equal(t, http.MethodGet, provider.request(0).method)
			require.Equal(t, "/api/v1/tickets/1892000000099004", provider.request(0).path)
			if test.isRetry {
				requireRetry(t, err, sdkgo.FailureAvailability)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, "1892000000099004", result.Receipt.ProviderObjectID)
		})
	}
}

// TestCreateTicketNeverResendsA2xx covers 2xx answers that cannot be used: with no readable ticket ID
// the write was still accepted, so it selects uncertain instead of resending.
func TestCreateTicketNeverResendsA2xx(t *testing.T) {
	for _, test := range []struct {
		name     string
		reply    func(http.ResponseWriter)
		kind     sdkgo.FailureKind
		maxBytes int64
	}{
		{name: "no ticket ID", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"id":"SENTINEL"}`)
		}, kind: sdkgo.FailureProtocol},
		{name: "reflected token", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"id":"1","subject":"`+testAccessToken+`"}`)
		}, kind: sdkgo.FailureProtocol},
		{name: "oversized", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099004", "Open", "Open"))
		}, kind: sdkgo.FailureResponseTooLarge, maxBytes: 64},
		{name: "body breaks after the headers", reply: func(response http.ResponseWriter) {
			response.Header().Set("Content-Length", "4096")
			response.WriteHeader(http.StatusOK)
			_, err := io.WriteString(response, `{"id":"1892000000099004"`)
			require.NoError(t, err)
			response.(http.Flusher).Flush()
		}, kind: sdkgo.FailureTransport},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
			client, err := desk.New(desk.Config{OrgID: testOrganizationID, MaxResponseBytes: test.maxBytes}, testCredentialProvider(),
				desk.WithAPIBaseURL(provider.URL+"/api/v1"))
			require.NoError(t, err)
			ctx := newDeskDexContext("create-2xx-" + test.name)
			result, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
			require.NoError(t, err)
			require.Equal(t, desk.CreateTicketBranchUncertain, result.Branch, "a 2xx accepted the ticket")
			require.Equal(t, test.kind, result.Failure.Kind)
			requireNoSentinel(t, result)
			require.Equal(t, 1, provider.requestCount())
			require.Equal(t, singledispatch.StateClaimed, recordedMarker(t, ctx).State)
		})
	}
}

// TestCreateTicketWithAnUnsavedTicketIDNeverRetriesAFailedReadBack covers a failed provider-ID
// checkpoint: a retry could resend, so a transient read-back failure selects uncertain.
func TestCreateTicketWithAnUnsavedTicketIDNeverRetriesAFailedReadBack(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusOK, `{"id":"1892000000099004","subject":"SENTINEL"}`)
			return
		}
		writeJSON(t, response, http.StatusInternalServerError, `{"errorCode":"INTERNAL_SERVER_ERROR"}`)
	})
	ctx := newDeskDexContext("create-unsaved-id")
	ctx.acceptedHeartbeatLimit = 1
	result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Empty(t, recordedMarker(t, ctx).ProviderID, "only the claim was recorded")
}

// TestCreateTicketCountsEarlierSendsAfterACanResendMarker covers X1: after an unknown outcome and a
// 429 resend, the next attempt is SendAllowed but still knows an earlier send may have applied.
func TestCreateTicketCountsEarlierSendsAfterACanResendMarker(t *testing.T) {
	for _, test := range []struct {
		name  string
		third func(t *testing.T, ctx *deskDexContext, provider *recordingDesk)
	}{
		{name: "refusal is uncertain", third: func(t *testing.T, ctx *deskDexContext, provider *recordingDesk) {
			result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
			require.NoError(t, err)
			require.Equal(t, desk.CreateTicketBranchUncertain, result.Branch, "the first send may have created the ticket")
		}},
		{name: "revoked credentials are uncertain", third: func(t *testing.T, ctx *deskDexContext, provider *recordingDesk) {
			revoked := failingCredentialProvider{err: sdkgo.NewReauthorizationRequiredError(errors.New("invalid_code"))}
			client, err := desk.New(desk.Config{OrgID: testOrganizationID}, revoked, desk.WithAPIBaseURL(provider.URL+"/api/v1"))
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
			require.NoError(t, err)
			require.Equal(t, desk.CreateTicketBranchUncertain, result.Branch)
			require.Zero(t, ctx.heartbeatCount, "notSent would hide the earlier possible send")
		}},
		{name: "transient refresh after 401 retries without canResend", third: func(t *testing.T, ctx *deskDexContext, provider *recordingDesk) {
			credentials := &failingRejectionRefreshCredentialProvider{err: errors.New("Zoho token endpoint returned HTTP 503")}
			client, err := desk.New(desk.Config{OrgID: testOrganizationID}, credentials, desk.WithAPIBaseURL(provider.URL+"/api/v1"))
			require.NoError(t, err)
			_, err = sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
			retry := requireRetry(t, err, sdkgo.FailureAuthentication)
			require.Equal(t, "Zoho Desk rejected the access token (HTTP 401); credential refresh is temporarily unavailable", retry.Failure.Message)
			require.Equal(t, singledispatch.StateClaimed, recordedMarker(t, ctx).State, "the claim stays, so the next attempt counts it")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				switch index {
				case 0:
					writeJSON(t, response, http.StatusInternalServerError, `{"errorCode":"INTERNAL_SERVER_ERROR"}`)
				case 1:
					writeJSON(t, response, http.StatusTooManyRequests, `{"errorCode":"TOO_MANY_REQUESTS"}`)
				case 2:
					if test.name == "transient refresh after 401 retries without canResend" {
						writeJSON(t, response, http.StatusUnauthorized, `{"errorCode":"INVALID_OAUTH"}`)
						return
					}
					writeJSON(t, response, http.StatusUnprocessableEntity, `{"errorCode":"INVALID_DATA"}`)
				default:
					t.Fatal("no fourth request is expected")
				}
			})
			client := newDeskClient(t, provider.URL)
			ctx := newDeskDexContext("create-x1-" + test.name)
			_, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
			requireRetry(t, err, sdkgo.FailureAvailability)
			ctx = ctx.nextAttempt()
			_, err = sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
			requireRetry(t, err, sdkgo.FailureRateLimit)
			marker := recordedMarker(t, ctx)
			require.Equal(t, singledispatch.StateCanResend, marker.State)
			require.Equal(t, int32(1), marker.SendCount, "the first send still counts")
			test.third(t, ctx.nextAttempt(), provider)
		})
	}
}

// TestCreateTicketRetriesATransientRefreshAfterAFirst401 covers a first send refused with 401 whose
// refresh fails transiently: the 401 created nothing, so canResend is recorded and the Step retries.
func TestCreateTicketRetriesATransientRefreshAfterAFirst401(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, `{"errorCode":"INVALID_OAUTH"}`)
	})
	credentials := &failingRejectionRefreshCredentialProvider{err: errors.New("Zoho token endpoint returned HTTP 503")}
	client, err := desk.New(desk.Config{OrgID: testOrganizationID}, credentials, desk.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)
	ctx := newDeskDexContext("create-first-401-transient")
	_, err = sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureAuthentication)
	require.Equal(t, singledispatch.StateCanResend, recordedMarker(t, ctx).State)
	require.Equal(t, 1, provider.requestCount())

	revoked := &failingRejectionRefreshCredentialProvider{err: sdkgo.NewReauthorizationRequiredError(errors.New("invalid_code"))}
	client, err = desk.New(desk.Config{OrgID: testOrganizationID}, revoked, desk.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)
	fresh := newDeskDexContext("create-first-401-revoked")
	result, err := sdkgo.RunMutation(fresh, client.CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchProviderRejected, result.Branch, "a revoked refresh after a first 401 created nothing")
	require.Equal(t, singledispatch.StateCanResend, recordedMarker(t, fresh).State)
}

func TestCreateTicketRetries429AndUnsentRequests(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "34")
		writeJSON(t, response, http.StatusTooManyRequests, `{"errorCode":"TOO_MANY_REQUESTS","message":"SENTINEL"}`)
	})
	ctx := newDeskDexContext("create-rate-limited")
	_, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 34*time.Second, retryAfter.After)
	require.Equal(t, singledispatch.StateCanResend, recordedMarker(t, ctx).State, "a 429 records canResend so the retry may send")

	refused := newDeskDexContext("create-refused")
	_, err = sdkgo.RunMutation(refused, newDeskClient(t, closedLoopbackURL(t)).CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Equal(t, singledispatch.StateCanResend, recordedMarker(t, refused).State, "a refused connection sent nothing")

	next := ctx.nextAttempt()
	created := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099003", "Open", "Open"))
	})
	result, err := sdkgo.RunMutation(next, newDeskClient(t, created.URL).CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchCreated, result.Branch)
}

func TestCreateTicketRetriesAfter503AndRecordsCanResend(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, attempt int) {
		if attempt == 0 {
			response.Header().Set("Retry-After", "2")
			writeJSON(t, response, http.StatusServiceUnavailable, `{"errorCode":"SERVICE_UNAVAILABLE"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099003", "Open", "Open"))
	})
	ctx := newDeskDexContext("create-service-unavailable")
	client := newDeskClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 2*time.Second, retryAfter.After)
	require.Equal(t, singledispatch.StateCanResend, recordedMarker(t, ctx).State, "a 503 records canResend before retry")
	require.Equal(t, 1, provider.requestCount())

	result, err := sdkgo.RunMutation(ctx.nextAttempt(), client.CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())
}

// TestCreateTicketRetriesWithoutSendingAfterAnUntrustedCheckpoint covers markers written by
// v0.25.0 and earlier, which the SDK fence treats as foreign and fails closed on.
func TestCreateTicketRetriesWithoutSendingAfterAnUntrustedCheckpoint(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	first := newDeskDexContext("create-replay")
	first.recordedHeartbeat = json.RawMessage(`{"zohoDeskDispatchedCallId":"earlier"}`)
	_, err := sdkgo.RunMutation(first.nextAttempt(), newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
	retry := requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, "the dispatch checkpoint could not be read; nothing was sent in this attempt", retry.Failure.Message)

	unrecordable := newDeskDexContext("create-unrecordable")
	unrecordable.rejectsHeartbeat = true
	_, err = sdkgo.RunMutation(unrecordable, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, provider.requestCount())
}

// TestCreateTicketAfterAnEarlierPossibleSendStaysUncertainWhenCredentialsFail checks the fence
// before credentials, so a revoked authorization after a possible send is never reported as "nothing created".
func TestCreateTicketAfterAnEarlierPossibleSendStaysUncertainWhenCredentialsFail(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	first := newDeskDexContext("create-revoked-after-send")
	_, err := sdkgo.RunMutation(first, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureTransport)

	revoked := failingCredentialProvider{err: sdkgo.NewReauthorizationRequiredError(errors.New("invalid_code"))}
	client, err := desk.New(desk.Config{OrgID: testOrganizationID}, revoked, desk.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchUncertain, result.Branch)

	outage, err := desk.New(desk.Config{OrgID: testOrganizationID}, failingCredentialProvider{err: errors.New("Zoho token endpoint returned HTTP 503")},
		desk.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(first.nextAttempt(), outage.CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureAuthentication)

	fresh := newDeskDexContext("create-revoked")
	result, err = sdkgo.RunMutation(fresh, client.CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchProviderRejected, result.Branch, "with no earlier send, a revoked authorization created nothing")
	require.Equal(t, singledispatch.StateNotSent, recordedMarker(t, fresh).State)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateTicketRetriesAfterUnauthorizedWithARefreshedToken(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		require.Equal(t, "Zoho-oauthtoken 1000.rejected-token", request.Header.Get("Authorization"))
		writeJSON(t, response, http.StatusUnauthorized, `{"errorCode":"INVALID_OAUTH","message":"SENTINEL The OAuth Token you provided is invalid."}`)
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := desk.New(desk.Config{OrgID: testOrganizationID}, credentials, desk.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)

	ctx := newDeskDexContext("create-refresh")
	_, err = sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
	retry := requireRetry(t, err, sdkgo.FailureAuthentication)
	require.Equal(t, "Zoho Desk rejected the prior access token (HTTP 401); retry with its replacement", retry.Failure.Message)
	require.Equal(t, 1, provider.requestCount(), "the fenced write is never resent inside one attempt")
	require.Equal(t, 1, credentials.forcedRefreshes)
	require.Equal(t, singledispatch.StateCanResend, recordedMarker(t, ctx).State, "a 401 proves nothing was created")
}

// TestCreateTicketRetriesAConnectionThatNeverCarriedTheRequest fails the TLS handshake against a
// plain-HTTP server, so no connection is obtained and the request provably never left.
func TestCreateTicketRetriesAConnectionThatNeverCarriedTheRequest(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	ctx := newDeskDexContext("create-handshake")
	client := newDeskClient(t, strings.Replace(provider.URL, "http://", "https://", 1))
	_, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Equal(t, singledispatch.StateCanResend, recordedMarker(t, ctx).State, "the retry may send")
	require.Zero(t, provider.requestCount())
}

func TestCreateTicketRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, change := range map[string]func(*desk.CreateTicketInput){
		"blank subject":        func(input *desk.CreateTicketInput) { input.Subject = " " },
		"long subject":         func(input *desk.CreateTicketInput) { input.Subject = strings.Repeat("s", 256) },
		"blank description":    func(input *desk.CreateTicketInput) { input.Description = "" },
		"escaped too long":     func(input *desk.CreateTicketInput) { input.Description = strings.Repeat("<", 20000) },
		"missing department":   func(input *desk.CreateTicketInput) { input.DepartmentID = "" },
		"no contact":           func(input *desk.CreateTicketInput) { input.Contact = desk.TicketContactInput{} },
		"contact ID and email": func(input *desk.CreateTicketInput) { input.ContactID = testContactID },
		"display address":      func(input *desk.CreateTicketInput) { input.Contact.Email = "Jane <jane@example.com>" },
		"padded first name":    func(input *desk.CreateTicketInput) { input.Contact.FirstName = " Jane" },
		"comma status":         func(input *desk.CreateTicketInput) { input.Status = "Open,Closed" },
		"padded priority":      func(input *desk.CreateTicketInput) { input.Priority = "High " },
		"assignee name":        func(input *desk.CreateTicketInput) { input.AssigneeID = "jade" },
		"invalid UTF-8":        func(input *desk.CreateTicketInput) { input.Description = strings.Repeat("\xff", 3) },
	} {
		input := validCreateTicketInput()
		change(&input)
		ctx := newDeskDexContext("invalid-create")
		result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, desk.CreateTicketBranchDefect, result.Branch, name)
		require.Zero(t, ctx.heartbeatCount, name)
	}
}
