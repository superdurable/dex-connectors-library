// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestNewRequiresANumericOrgIDAndValidOptions(t *testing.T) {
	for name, test := range map[string]struct {
		config  desk.Config
		options []desk.Option
		message string
	}{
		"blank orgId":     {config: desk.Config{}, message: "choose the organization with the Zoho Desk organization picker"},
		"spaces":          {config: desk.Config{OrgID: "   "}, message: "orgId is required"},
		"portal name":     {config: desk.Config{OrgID: "zylker"}, message: "numeric ID"},
		"negative limit":  {config: desk.Config{OrgID: testOrganizationID, MaxResponseBytes: -1}, message: "cannot be negative"},
		"plain HTTP host": {config: desk.Config{OrgID: testOrganizationID}, options: []desk.Option{desk.WithAPIBaseURL("http://desk.example.com/api/v1")}, message: "HTTPS"},
		"nil option":      {config: desk.Config{OrgID: testOrganizationID}, options: []desk.Option{nil}, message: "option is nil"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := desk.New(test.config, testCredentialProvider(), test.options...)
			require.ErrorContains(t, err, test.message)
		})
	}
	_, err := desk.New(desk.Config{OrgID: testOrganizationID}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	client, err := desk.New(desk.Config{OrgID: " " + testOrganizationID + " "}, testCredentialProvider())
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestRequestsCarryTheOrgIDHeaderAndTheZohoOAuthToken(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(testTicketID, "Open", "Open"))
		default:
			writeJSON(t, response, http.StatusOK, `{"data":[]}`)
		}
	})
	result, err := sdkgo.RunQuery(newDeskDexContext("headers"), newDeskClient(t, provider.URL).GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	require.NoError(t, err)
	require.Equal(t, desk.GetTicketBranchFound, result.Branch)
	for index := 0; index < provider.requestCount(); index++ {
		request := provider.request(index)
		require.Equal(t, "Zoho-oauthtoken "+testAccessToken, request.header.Get("Authorization"))
		require.Equal(t, testOrganizationID, request.header.Get("orgId"))
		require.Equal(t, "application/json", request.header.Get("Accept"))
	}
	require.Equal(t, map[string]string{"requestCredits": "1", "remainingCredits": "49950"}, result.Receipt.Metadata)
}

func TestEachDataCenterSendsRequestsOnlyToItsOwnDeskHost(t *testing.T) {
	for _, dataCenter := range desk.DataCenters() {
		t.Run(dataCenter.Name, func(t *testing.T) {
			var requestedURLs []string
			httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestedURLs = append(requestedURLs, request.URL.String())
				require.Equal(t, []string{testOrganizationID}, request.Header["orgId"], "the header is sent as Zoho Desk spells it")
				return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"errorCode":"URL_NOT_FOUND"}`)), Request: request}, nil
			})}
			credentials := sdkgo.StaticCredentialProvider[desk.Credentials]{deskConnection: testCredentials(dataCenter.AuthMethodID)}
			client, err := desk.New(desk.Config{OrgID: testOrganizationID}, credentials, desk.WithHTTPClient(httpClient))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newDeskDexContext("host-"+dataCenter.AuthMethodID), client.GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
			require.NoError(t, err)
			require.Equal(t, desk.GetTicketBranchNotFound, result.Branch)
			require.Equal(t, []string{dataCenter.DeskURL + "/api/v1/tickets/" + testTicketID + "?include=contacts"}, requestedURLs)
		})
	}
}

func TestUnauthorizedRequestRefreshesOnceAndResendsWithTheReplacement(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			require.Equal(t, "Zoho-oauthtoken 1000.rejected-token", request.Header.Get("Authorization"))
			writeJSON(t, response, http.StatusUnauthorized, `{"errorCode":"INVALID_OAUTH","message":"SENTINEL The OAuth Token you provided is invalid."}`)
			return
		}
		require.Equal(t, "Zoho-oauthtoken 1000.replacement-token", request.Header.Get("Authorization"))
		writeValue(t, response, http.StatusOK, ticketJSON("1892000000099001", "Open", "Open"))
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := desk.New(desk.Config{OrgID: testOrganizationID}, credentials, desk.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)

	result, err := sdkgo.RunMutation(newDeskDexContext("refresh-once"), client.CreateTicket(), deskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, desk.CreateTicketBranchCreated, result.Branch, "a 401 proves Zoho Desk created nothing, so one resend is safe")
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
}

func TestSecondUnauthorizedResponseIsTerminalWithoutARefreshLoop(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, `{"errorCode":"INVALID_OAUTH","message":"SENTINEL invalid"}`)
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := desk.New(desk.Config{OrgID: testOrganizationID}, credentials, desk.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newDeskDexContext("no-refresh-loop"), client.GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	require.NoError(t, err)
	require.Equal(t, desk.GetTicketBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "Zoho Desk rejected the access token (HTTP 401); reconnect the connection [INVALID_OAUTH]", result.Failure.Message)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
	requireNoSentinel(t, result)
}

func TestFailuresNameErrorCodesAndFieldsButNeverProviderMessages(t *testing.T) {
	for name, test := range map[string]struct {
		status  int
		body    string
		kind    sdkgo.FailureKind
		message string
	}{
		"invalid data": {status: http.StatusUnprocessableEntity, kind: sdkgo.FailureValidation,
			body:    `{"errorCode":"INVALID_DATA","message":"SENTINEL","errors":[{"fieldName":"/departmentId","errorType":"invalid","errorMessage":"SENTINEL"},{"fieldName":"/contactId","errorType":"missing"},{"fieldName":"SENTINEL text","errorType":"invalid"}]}`,
			message: "Zoho Desk rejected the request (HTTP 422) [INVALID_DATA; errors: /contactId=missing, /departmentId=invalid]"},
		"organization mismatch": {status: http.StatusForbidden, kind: sdkgo.FailureAuthorization,
			body:    `{"errorCode":"OAUTH_ORG_MISMATCH","message":"SENTINEL The OAuthToken is not valid for specified organization."}`,
			message: "Zoho Desk refused the request (HTTP 403): the token is bound to another organization; set orgId to the organization chosen at Connect, or reconnect [OAUTH_ORG_MISMATCH]"},
		"scope mismatch": {status: http.StatusForbidden, kind: sdkgo.FailureAuthorization,
			body:    `{"errorCode":"SCOPE_MISMATCH","message":"SENTINEL"}`,
			message: "Zoho Desk refused the request (HTTP 403): the token lacks a required scope; reconnect and accept every requested scope [SCOPE_MISMATCH]"},
		"license": {status: http.StatusForbidden, kind: sdkgo.FailureAuthorization,
			body:    `{"errorCode":"LICENSE_ACCESS_LIMITED","message":"SENTINEL","editionType":"FREE"}`,
			message: "Zoho Desk refused the request (HTTP 403) [LICENSE_ACCESS_LIMITED]"},
		"message only": {status: http.StatusBadRequest, kind: sdkgo.FailureValidation,
			body: `{"message":"SENTINEL Error while Processing Request."}`, message: "Zoho Desk rejected the request (HTTP 400)"},
		"code with text": {status: http.StatusMethodNotAllowed, kind: sdkgo.FailureProviderRejection,
			body: `{"errorCode":"SENTINEL text"}`, message: "Zoho Desk rejected the request (HTTP 405)"},
		"reflected token": {status: http.StatusBadRequest, kind: sdkgo.FailureValidation,
			body: `{"errorCode":"INVALID_DATA","message":"` + testAccessToken + `"}`, message: "Zoho Desk rejected the request (HTTP 400)"},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newDeskDexContext("failure"), newDeskClient(t, provider.URL).GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
			require.NoError(t, err)
			require.Equal(t, desk.GetTicketBranchProviderRejected, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, test.message, result.Failure.Message)
			requireNoSentinel(t, result)
		})
	}
}

func TestRateLimitedReadRetriesAfterRetryAfter(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "34")
		writeJSON(t, response, http.StatusTooManyRequests, `{"errorCode":"THRESHOLD_EXCEEDED","message":"SENTINEL"}`)
	})
	_, err := sdkgo.RunQuery(newDeskDexContext("rate-limited"), newDeskClient(t, provider.URL).GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	retry := requireRetry(t, err, sdkgo.FailureRateLimit)
	require.Equal(t, "Zoho Desk rate limited the request (HTTP 429) [THRESHOLD_EXCEEDED]", retry.Failure.Message)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 34*time.Second, retryAfter.After)
}

func TestReadOutagesAndTransportFailuresRetry(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusInternalServerError, `{"errorCode":"INTERNAL_SERVER_ERROR","message":"SENTINEL"}`)
			return
		}
		dropConnection(t, response)
	})
	client := newDeskClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newDeskDexContext("server-error"), client.GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	requireRetry(t, err, sdkgo.FailureAvailability)
	_, err = sdkgo.RunQuery(newDeskDexContext("lost-response"), client.GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	requireRetry(t, err, sdkgo.FailureTransport)
	_, err = sdkgo.RunQuery(newDeskDexContext("refused"), newDeskClient(t, closedLoopbackURL(t)).GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	requireRetry(t, err, sdkgo.FailureTransport)
}

func TestRedirectsAreNeverFollowed(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		http.Redirect(response, request, "https://accounts.zoho.eu/signin", http.StatusFound)
	})
	result, err := sdkgo.RunQuery(newDeskDexContext("redirect"), newDeskClient(t, provider.URL).GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	require.NoError(t, err)
	require.Equal(t, desk.GetTicketBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "data center")
	require.Equal(t, 1, provider.requestCount())
}

func TestResponsesThatReflectTheTokenOrExceedTheLimitAreNeverReturned(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		value := ticketJSON(testTicketID, "Open", "Open")
		value["subject"] = testAccessToken
		writeValue(t, response, http.StatusOK, value)
	})
	result, err := sdkgo.RunQuery(newDeskDexContext("reflected"), newDeskClient(t, provider.URL).GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	require.NoError(t, err)
	require.Equal(t, desk.GetTicketBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	requireNoSentinel(t, result)

	large := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"`+testTicketID+`","description":"`+strings.Repeat("a", 2048)+`"}`)
	})
	client, err := desk.New(desk.Config{OrgID: testOrganizationID, MaxResponseBytes: 1024}, testCredentialProvider(), desk.WithAPIBaseURL(large.URL+"/api/v1"))
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(newDeskDexContext("oversized"), client.GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	require.NoError(t, err)
	require.Equal(t, desk.GetTicketBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestCredentialFailuresSendNothing(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, test := range map[string]struct {
		credentials desk.CredentialSource
		branch      sdkgo.BranchID
		isRetry     bool
	}{
		"reauthorization required": {credentials: failingCredentialProvider{err: sdkgo.NewReauthorizationRequiredError(errors.New("invalid_code"))}, branch: desk.GetTicketBranchProviderRejected},
		"refresh outage":           {credentials: failingCredentialProvider{err: errors.New("Zoho token endpoint returned HTTP 503")}, isRetry: true},
		"unknown data center": {credentials: sdkgo.StaticCredentialProvider[desk.Credentials]{deskConnection: testCredentials("zoho-cn-oauth")},
			branch: desk.GetTicketBranchDefect},
		"token with a space": {credentials: sdkgo.StaticCredentialProvider[desk.Credentials]{deskConnection: {AuthMethodID: desk.USDataCenterAuthMethodID, AccessToken: sdkgo.NewSecretString("two words")}},
			branch: desk.GetTicketBranchDefect},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := desk.New(desk.Config{OrgID: testOrganizationID}, test.credentials, desk.WithAPIBaseURL(provider.URL+"/api/v1"))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newDeskDexContext("credentials"), client.GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
			if test.isRetry {
				requireRetry(t, err, sdkgo.FailureAuthentication)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
		})
	}
	require.Zero(t, provider.requestCount())
}

// failingCredentialProvider fails every resolution with err, as a broken refresh would.
type failingCredentialProvider struct {
	err error
}

func (provider failingCredentialProvider) Resolve(sdkgo.Call) (desk.Credentials, error) {
	return desk.Credentials{}, provider.err
}

func (provider failingCredentialProvider) ResolveWithRefresh(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[desk.Credentials],
) (desk.Credentials, error) {
	return desk.Credentials{}, provider.err
}
