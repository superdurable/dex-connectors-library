// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
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
	require.JSONEq(t, `{"zohoDeskDispatchedCallId":"`+string(result.Receipt.CallID)+`"}`, string(ctx.recordedHeartbeat),
		"the dispatch marker is recorded before the request")
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

func TestCreateTicketNeverResendsARequestWhoseOutcomeIsUnknown(t *testing.T) {
	for _, test := range []struct {
		name   string
		reply  func(http.ResponseWriter)
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "server error", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"errorCode":"INTERNAL_SERVER_ERROR","message":"SENTINEL"}`)
		}, branch: desk.CreateTicketBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "gateway timeout", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusGatewayTimeout, ``)
		}, branch: desk.CreateTicketBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) },
			branch: desk.CreateTicketBranchUncertain, kind: sdkgo.FailureTransport},
		{name: "malformed ticket", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"id":"SENTINEL"}`)
		}, branch: desk.CreateTicketBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "reflected token", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"id":"1","subject":"`+testAccessToken+`"}`)
		}, branch: desk.CreateTicketBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "invalid data", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusUnprocessableEntity, `{"errorCode":"INVALID_DATA","message":"SENTINEL","errors":[{"fieldName":"/departmentId","errorType":"invalid"}]}`)
		}, branch: desk.CreateTicketBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "scope mismatch", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusForbidden, `{"errorCode":"SCOPE_MISMATCH"}`)
		}, branch: desk.CreateTicketBranchProviderRejected, kind: sdkgo.FailureAuthorization},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
			ctx := newDeskDexContext("create-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
			require.NoError(t, err, "only a provable non-application is retried")
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			requireNoSentinel(t, result)
			require.NotEmpty(t, ctx.recordedHeartbeat, "the marker stays, so a replayed attempt sends nothing")
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestCreateTicketRetriesOnlyWhatZohoDeskProvablyDidNotApply(t *testing.T) {
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
	require.Nil(t, ctx.recordedHeartbeat, "a 429 clears the marker so the retry may send")
	require.Equal(t, 2, ctx.heartbeatCount)

	refused := newDeskDexContext("create-refused")
	_, err = sdkgo.RunMutation(refused, newDeskClient(t, closedLoopbackURL(t)).CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Nil(t, refused.recordedHeartbeat, "a refused connection sent nothing")

	next := ctx.nextAttempt()
	created := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099003", "Open", "Open"))
	})
	result, err := sdkgo.RunMutation(next, newDeskClient(t, created.URL).CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchCreated, result.Branch)
}

func TestCreateTicketAfterAnEarlierDispatchSendsNothing(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	first := newDeskDexContext("create-replay")
	first.recordedHeartbeat = json.RawMessage(`{"zohoDeskDispatchedCallId":"earlier"}`)
	result, err := sdkgo.RunMutation(first.nextAttempt(), newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchUncertain, result.Branch)
	require.Equal(t, "an earlier attempt of this Step may have sent the request, so it is not sent again", result.Failure.Message)

	unrecordable := newDeskDexContext("create-unrecordable")
	unrecordable.rejectsHeartbeat = true
	_, err = sdkgo.RunMutation(unrecordable, newDeskClient(t, provider.URL).CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, provider.requestCount())
}

// TestCreateTicketAfterAnEarlierDispatchStaysUncertainWhenCredentialsFail checks the marker before
// credentials, so a revoked authorization after a possible send is never reported as "nothing created".
func TestCreateTicketAfterAnEarlierDispatchStaysUncertainWhenCredentialsFail(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	revoked := failingCredentialProvider{err: sdkgo.NewReauthorizationRequiredError(errors.New("invalid_code"))}
	client, err := desk.New(desk.Config{OrgID: testOrganizationID}, revoked, desk.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)
	first := newDeskDexContext("create-revoked-after-send")
	first.recordedHeartbeat = json.RawMessage(`{"zohoDeskDispatchedCallId":"earlier"}`)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchUncertain, result.Branch)

	fresh := newDeskDexContext("create-revoked")
	result, err = sdkgo.RunMutation(fresh, client.CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchProviderRejected, result.Branch, "with no earlier send, a revoked authorization created nothing")
	require.Zero(t, fresh.heartbeatCount, "no marker is recorded before credentials resolve")
}

// TestCreateTicketRetriesAConnectionThatNeverCarriedTheRequest fails the TLS handshake against a
// plain-HTTP server, so no connection is obtained and the request provably never left.
func TestCreateTicketRetriesAConnectionThatNeverCarriedTheRequest(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	ctx := newDeskDexContext("create-handshake")
	client := newDeskClient(t, strings.Replace(provider.URL, "http://", "https://", 1))
	_, err := sdkgo.RunMutation(ctx, client.CreateTicket(), deskConnection, validCreateTicketInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Nil(t, ctx.recordedHeartbeat, "the marker is cleared, so the retry may send")
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
