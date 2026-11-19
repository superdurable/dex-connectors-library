// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	goContext "context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/go"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// failingCredentials fails every credential lookup with err.
type failingCredentials struct{ err error }

func (credentials failingCredentials) Resolve(sdkgo.Call) (oauth2http.Credentials, error) {
	return oauth2http.Credentials{}, credentials.err
}

func (credentials failingCredentials) ResolveWithRefresh(goContext.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[oauth2http.Credentials]) (oauth2http.Credentials, error) {
	return oauth2http.Credentials{}, credentials.err
}

func newClientWithCredentials(t *testing.T, session *testSession, credentials oauth2http.CredentialSource) *oauth2http.Client {
	t.Helper()
	client, err := oauth2http.New(session.server.config(), credentials, oauth2http.WithHTTPClient(session.server.Client()))
	require.NoError(t, err)
	return client
}

func TestSendRequestRetriesATransientCredentialFailure(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	for _, transient := range []error{projectconfig.ErrCredentialBrokerUnavailable, projectconfig.ErrExchangePending, projectconfig.ErrOutcomeUnknown,
		errors.New("connection is not configured"), errors.New("unlisted resolve failure")} {
		client := newClientWithCredentials(t, session, failingCredentials{err: transient})
		_, err := sendRequest(t, client, "transient-"+transient.Error(), oauth2http.SendRequestInput{Method: http.MethodPost, Path: "/orders"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "%v never selects defect", transient)
		require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	}
	require.Empty(t, session.server.requests())
}

func TestSendRequestSelectsUncertainForATerminalCredentialFailureOfALaterWriteAttempt(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	for name, terminal := range map[string]error{
		"object not found": projectconfig.ErrObjectNotFound, "not selected": projectconfig.ErrConnectionNotSelected,
		"broker rejected": projectconfig.ErrCredentialBrokerRejected,
	} {
		client := newClientWithCredentials(t, session, failingCredentials{err: terminal})
		for method, expected := range map[string]sdkgo.BranchID{
			http.MethodGet: oauth2http.SendRequestBranchDefect, http.MethodPost: oauth2http.SendRequestBranchUncertain,
			http.MethodDelete: oauth2http.SendRequestBranchUncertain,
		} {
			context := testsupport.NewDexContext("oauth2-http-flow", "later-"+name+method)
			context.AttemptNo = 2
			result, err := sdkgo.RunMutation(context, client.SendRequest(), testConnection, oauth2http.SendRequestInput{Method: method, Path: "/orders"})
			require.NoError(t, err, method)
			require.Equal(t, expected, result.Branch, "%s %s: a later write may follow a processed one", name, method)
		}
		first, err := sendRequest(t, client, "first-post-"+name, oauth2http.SendRequestInput{Method: http.MethodPost, Path: "/orders"})
		require.NoError(t, err)
		require.Equal(t, oauth2http.SendRequestBranchDefect, first.Branch, "nothing was sent before the first attempt")
	}
	reauthorization := newClientWithCredentials(t, session, failingCredentials{err: projectconfig.ErrReauthorizationRequired})
	first, err := sendRequest(t, reauthorization, "first-post-reauthorization", oauth2http.SendRequestInput{Method: http.MethodPost, Path: "/orders"})
	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchRejected, first.Branch, "a revoked authorization needs a person")
	require.Empty(t, session.server.requests())
}

func TestSendRequestSelectsUncertainForADefiniteRefusalOfALaterWriteAttempt(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	session.server.api = func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnprocessableEntity)
	}
	for _, test := range []struct {
		method   string
		attempt  int32
		expected sdkgo.BranchID
	}{
		{http.MethodPost, 1, oauth2http.SendRequestBranchRejected},
		{http.MethodPost, 2, oauth2http.SendRequestBranchUncertain},
		{http.MethodPut, 3, oauth2http.SendRequestBranchUncertain},
		{http.MethodGet, 2, oauth2http.SendRequestBranchRejected},
	} {
		context := testsupport.NewDexContext("oauth2-http-flow", "refused-"+test.method)
		context.AttemptNo = test.attempt
		result, err := sdkgo.RunMutation(context, session.client.SendRequest(), testConnection, oauth2http.SendRequestInput{Method: test.method, Path: "/orders"})
		require.NoError(t, err)
		require.Equal(t, test.expected, result.Branch, "%s on attempt %d", test.method, test.attempt)
		require.Equal(t, http.StatusUnprocessableEntity, result.Value.StatusCode)
	}
}

func TestPostNonIdempotentRetriesATransientCredentialFailureBeforeAnySend(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	client := newClientWithCredentials(t, session, failingCredentials{err: projectconfig.ErrCredentialBrokerUnavailable})
	context := singledispatchtest.NewContext("oauth2-http-flow", "broker-unavailable")
	_, err := sdkgo.RunMutation(context, client.PostNonIdempotent(), testConnection, oauth2http.PostNonIdempotentInput{Path: "/orders"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, singledispatch.StateNotSent, lastMarker(t, context).State)
	require.Empty(t, session.server.requests())
}

func TestPostNonIdempotentNeverSendsWhileTheCheckpointCountsAnEarlierPossibleSend(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	throttled := true
	session.server.api = func(response http.ResponseWriter, _ *http.Request) {
		if throttled {
			response.WriteHeader(http.StatusTooManyRequests)
			return
		}
		response.WriteHeader(http.StatusCreated)
	}
	context := singledispatchtest.NewContext("oauth2-http-flow", "counted-send")
	_, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	marker := lastMarker(t, context)
	// A canResend marker that still counts one possibly effective send keeps SendAllowed.
	context.AdvanceAttempt()
	require.NoError(t, context.SetPersistedCheckpoint(singledispatch.Marker{CallID: marker.CallID, Attempt: 2, State: singledispatch.StateCanResend, SendCount: 1}))
	context.AdvanceAttempt()
	throttled = false

	result, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders"})

	require.NoError(t, err)
	require.Equal(t, oauth2http.PostNonIdempotentBranchUncertain, result.Branch, "the send cap of one is already used")
	require.Len(t, session.server.requests(), 1, "only the throttled POST was sent")
}
