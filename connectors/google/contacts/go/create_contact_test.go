// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestCreateContactConfirmsOneSendAndReadsBackTheSavedContact(t *testing.T) {
	requests, posts := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, "names,emailAddresses", r.URL.Query().Get("personFields"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		if r.Method == http.MethodGet {
			require.Equal(t, "/v1/people/c123", r.URL.Path)
			_, err := w.Write([]byte(`{"resourceName":"people/c123","names":[{"displayName":"Ada Lovelace"}],"emailAddresses":[{"value":"ada@example.com"}]}`))
			require.NoError(t, err)
			return
		}
		posts++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/people:createContact", r.URL.Path)
		var body struct {
			Names []struct {
				GivenName  string `json:"givenName"`
				FamilyName string `json:"familyName"`
			} `json:"names"`
			EmailAddresses []struct {
				Value string `json:"value"`
			} `json:"emailAddresses"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Len(t, body.Names, 1)
		require.Equal(t, "Ada", body.Names[0].GivenName)
		require.Equal(t, "Lovelace", body.Names[0].FamilyName)
		require.Equal(t, "ada@example.com", body.EmailAddresses[0].Value)
		_, err := w.Write([]byte(`{"resourceName":"people/c123","names":[{"displayName":"Ada Lovelace"}],"emailAddresses":[{"value":"ada@example.com"}],"phoneNumbers":[{"value":"discard"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("create-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada", FamilyName: "Lovelace", EmailAddress: "ada@example.com"}
	created, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchCreated, created.Branch)
	require.Equal(t, Contact{ResourceName: "people/c123", DisplayName: "Ada Lovelace", EmailAddress: "ada@example.com"}, created.Value)
	ctx.AdvanceAttempt()
	readBack, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchCreated, readBack.Branch)
	require.Equal(t, created.Value, readBack.Value)
	require.Equal(t, 2, requests)
	require.Equal(t, 1, posts)
}

func TestCreateContactRetriesOnlyDefinitiveRejections(t *testing.T) {
	status, requests := http.StatusTooManyRequests, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, err := w.Write([]byte(`{"resourceName":"people/c124"}`))
			require.NoError(t, err)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("rate-limit-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada"}
	_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	status = http.StatusServiceUnavailable
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.ErrorAs(t, err, &retry)
	status = http.StatusOK
	ctx.AdvanceAttempt()
	created, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchCreated, created.Branch)
	require.Equal(t, 3, requests)
}

func TestCreateContactResendsUnknownOutcomesUntilTheSendCap(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader([]int{http.StatusInternalServerError, http.StatusConflict, http.StatusBadGateway}[min(requests-1, 2)])
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("uncertain-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada"}
	var retry *sdkgo.RetryError
	for range 2 {
		_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
		require.ErrorAs(t, err, &retry)
		ctx.AdvanceAttempt()
	}
	uncertain, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, uncertain.Branch)
	ctx.AdvanceAttempt()
	uncertain, err = sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, uncertain.Branch)
	require.Equal(t, 3, requests)
}

func TestCreateContactSelectsUncertainOnTheFirstUnknownOutcomeWithSendCapOne(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("cap-one-contact-flow", "create-contact-step")
	uncertain, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, CreateContactInput{GivenName: "Ada"}, sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, requests)
}

func TestCreateContactNeverResendsADefiniteRefusal(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadRequest)
		_, err := w.Write([]byte(`{"error":{"code":400,"status":"INVALID_ARGUMENT"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("rejected-contact-flow", "create-contact-step")
	rejected, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, CreateContactInput{GivenName: "Ada"})
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchProviderRejected, rejected.Branch)
	require.Equal(t, 1, requests)
	heartbeats := ctx.Heartbeats()
	require.Contains(t, string(heartbeats[len(heartbeats)-1]), `"state":"canResend"`)
}

func TestCreateContactSelectsUncertainForARefusalAfterAnUnknownOutcome(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader([]int{http.StatusInternalServerError, http.StatusForbidden}[min(requests-1, 1)])
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("refusal-after-unknown-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada"}
	_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, uncertain.Branch)
	require.Equal(t, 2, requests)
}

func TestCreateContactRetriesWhenTheCheckpointIsUnreadable(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++ }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("fail-closed-contact-flow", "create-contact-step")
	ctx.SetReadError(errors.New("heartbeat store unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, CreateContactInput{GivenName: "Ada"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Zero(t, requests)
}

func TestCreateContactRejectsInvalidNameBeforeDispatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++ }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("invalid-contact-flow", "create-contact-step")
	result, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, CreateContactInput{GivenName: " "})
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchDefect, result.Branch)
	require.Zero(t, requests)
}

func TestCreateContactNeverResendsAnAcceptedCreateWithoutAResourceName(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "unparsable body", status: http.StatusOK, body: `{"resourceName":`},
		{name: "missing resource name", status: http.StatusOK, body: `{"names":[]}`},
		{name: "accepted without body", status: http.StatusAccepted},
	} {
		t.Run(test.name, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				posts++
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			ctx := singledispatchtest.NewContext("accepted-contact-flow", "create-contact-step")
			result, err := sdkgo.RunMutation(ctx, newTestClient(t, server.URL).CreateContact(), testConnection, CreateContactInput{GivenName: "Ada"})
			require.NoError(t, err)
			require.Equal(t, CreateContactBranchUncertain, result.Branch)
			require.Equal(t, 1, posts)
		})
	}
}

type contactRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip contactRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type brokenContactBody struct{}

func (brokenContactBody) Read([]byte) (int, error) { return 0, errors.New("interrupted response") }
func (brokenContactBody) Close() error             { return nil }

func TestCreateContactNeverResendsASuccessWhoseBodyBreaks(t *testing.T) {
	posts := 0
	credentials := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	client, err := New(Config{BaseURL: "http://127.0.0.1:1"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials},
		WithHTTPClient(&http.Client{Transport: contactRoundTripper(func(request *http.Request) (*http.Response, error) {
			posts++
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: brokenContactBody{}, Request: request}, nil
		})}))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("broken-body-contact-flow", "create-contact-step")
	result, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, CreateContactInput{GivenName: "Ada"})
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, result.Branch)
	require.Equal(t, 1, posts)
}

func TestCreateContactReadsBackAResourceNameReturnedWithAnUnknownStatus(t *testing.T) {
	posts, reads := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			reads++
			_, _ = w.Write([]byte(`{"resourceName":"people/c125","names":[{"displayName":"Ada"}]}`))
			return
		}
		posts++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"resourceName":"people/c125"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("resource-name-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada"}
	_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchCreated, result.Branch)
	require.Equal(t, "people/c125", result.Value.ResourceName)
	require.Equal(t, 1, posts)
	require.Equal(t, 1, reads)
}

func TestCreateContactRetriesAReadBackWhoseBodyBreaks(t *testing.T) {
	posts, reads := 0, 0
	credentials := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	client, err := New(Config{BaseURL: "http://127.0.0.1:1"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials},
		WithHTTPClient(&http.Client{Transport: contactRoundTripper(func(request *http.Request) (*http.Response, error) {
			if request.Method == http.MethodPost {
				posts++
				return &http.Response{StatusCode: http.StatusConflict, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"resourceName":"people/c126"}`)), Request: request}, nil
			}
			reads++
			if reads == 1 {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: brokenContactBody{}, Request: request}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"resourceName":"people/c126","names":[{"displayName":"Ada"}]}`)), Request: request}, nil
		})}))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("broken-read-back-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada"}
	var retry *sdkgo.RetryError
	for range 2 {
		_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
		require.ErrorAs(t, err, &retry)
		ctx.AdvanceAttempt()
	}
	result, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchCreated, result.Branch)
	require.Equal(t, 1, posts)
	require.Equal(t, 2, reads)
}

func TestCreateContactReadsALateBodyInsideTheWriteWindow(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posts++
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"resourceName":"people/c127","names":[{"displayName":"Ada"}]}`))
	}))
	defer server.Close()
	ctx := singledispatchtest.NewContext("late-body-contact-flow", "create-contact-step")
	result, err := sdkgo.RunMutation(ctx, newTestClient(t, server.URL).CreateContact(), testConnection, CreateContactInput{GivenName: "Ada"})
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchCreated, result.Branch)
	require.Equal(t, "people/c127", result.Value.ResourceName)
	require.Equal(t, 1, posts)
}

func TestCreateContactTreatsAnUnreadableRefusalAsUnknown(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"` + testAccessToken + `"}`))
	}))
	defer server.Close()
	ctx := singledispatchtest.NewContext("unreadable-refusal-contact-flow", "create-contact-step")
	_, err := sdkgo.RunMutation(ctx, newTestClient(t, server.URL).CreateContact(), testConnection, CreateContactInput{GivenName: "Ada"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 1, posts)
}

// failingResolutionCredentials fails one numbered credential resolution transiently.
type failingResolutionCredentials struct {
	resolutions      int
	failedResolution int
}

func (source *failingResolutionCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	source.resolutions++
	if source.resolutions == source.failedResolution {
		return Credentials{}, errors.New("credential refresh unavailable")
	}
	return Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}, nil
}

func (source *failingResolutionCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return source.Resolve(call)
}

// Attempt 1 is unknown and attempt 2 gets 429, so attempt 3's credential failure follows a possible send.
func TestCreateContactPreSendRetryAfterAPossibleSendKeepsTheSendCount(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posts++
		w.WriteHeader([]int{http.StatusInternalServerError, http.StatusTooManyRequests, http.StatusInternalServerError}[min(posts-1, 2)])
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, &failingResolutionCredentials{failedResolution: 3})
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("pre-send-retry-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada"}
	var retry *sdkgo.RetryError
	for range 3 {
		_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
		require.ErrorAs(t, err, &retry)
		ctx.AdvanceAttempt()
	}
	// Attempts 1 and 3 count as possible sends, so attempt 4's unknown outcome is at the cap of 3.
	result, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, result.Branch)
	require.Equal(t, 3, posts)
}

// sequencedResolutionCredentials returns errs[n] for the nth resolution and valid credentials for nil.
type sequencedResolutionCredentials struct {
	errs        []error
	resolutions int
}

func (source *sequencedResolutionCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	err := source.errs[min(source.resolutions, len(source.errs)-1)]
	source.resolutions++
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}, nil
}

func (source *sequencedResolutionCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return source.Resolve(call)
}

func TestCreateContactClassifiesCredentialFailures(t *testing.T) {
	for _, test := range []struct {
		err        error
		isTerminal bool
	}{
		{err: sdkgo.NewReauthorizationRequiredError(errors.New("revoked")), isTerminal: true},
		{err: projectconfig.ErrReauthorizationRequired, isTerminal: true},
		{err: projectconfig.ErrObjectNotFound, isTerminal: true},
		{err: projectconfig.ErrConnectionNotSelected, isTerminal: true},
		{err: projectconfig.ErrCredentialBrokerRejected, isTerminal: true},
		{err: projectconfig.ErrCredentialBrokerUnavailable},
		{err: errors.New("connection is not configured")},
		{err: errors.New("unlisted resolve failure")},
	} {
		t.Run(test.err.Error(), func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				posts++
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			input := CreateContactInput{GivenName: "Ada"}
			var retry *sdkgo.RetryError

			firstSend, err := New(Config{BaseURL: server.URL}, &sequencedResolutionCredentials{errs: []error{test.err}})
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(singledispatchtest.NewContext("credential-first-contact-flow", "create-contact-step"), firstSend.CreateContact(), testConnection, input)
			if test.isTerminal {
				require.NoError(t, err)
				require.Equal(t, CreateContactBranchDefect, result.Branch)
			} else {
				require.ErrorAs(t, err, &retry)
			}
			require.Zero(t, posts)

			afterSend, err := New(Config{BaseURL: server.URL}, &sequencedResolutionCredentials{errs: []error{nil, test.err}})
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("credential-after-send-contact-flow", "create-contact-step")
			_, err = sdkgo.RunMutation(ctx, afterSend.CreateContact(), testConnection, input)
			require.ErrorAs(t, err, &retry)
			ctx.AdvanceAttempt()
			result, err = sdkgo.RunMutation(ctx, afterSend.CreateContact(), testConnection, input)
			if test.isTerminal {
				require.NoError(t, err)
				require.Equal(t, CreateContactBranchUncertain, result.Branch)
			} else {
				require.ErrorAs(t, err, &retry)
			}
			require.Equal(t, 1, posts)
		})
	}
}
