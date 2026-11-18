// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package brevo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

type providerContext struct{ dex.Context }

func (providerContext) FlowID() string              { return "brevo-provider-test" }
func (providerContext) StepExecutionID() string     { return "provider-step" }
func (providerContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerContext) Done() <-chan struct{}       { return nil }
func (providerContext) Err() error                  { return nil }
func (providerContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "brevo", Name: "brevo"}

func newProviderClient(t *testing.T, address string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: address + "/v3"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("brevo-test-key")}})
	require.NoError(t, err)
	return client
}

func TestCreateOrUpdateContact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/v3/contacts", r.URL.Path)
		require.Equal(t, "brevo-test-key", r.Header.Get("api-key"))
		require.Empty(t, r.URL.RawQuery)
		w.WriteHeader(http.StatusCreated)
		_, err := fmt.Fprint(w, `{"id":21}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).CreateOrUpdateContact(), testConnection, CreateOrUpdateContactInput{Email: "member@meridian.example", Attributes: map[string]string{"FNAME": "Meridian"}})
	require.NoError(t, err)
	require.Equal(t, CreateOrUpdateContactBranchSaved, result.Branch)
	require.Equal(t, ContactWriteResult{ID: 21, Email: "member@meridian.example", Created: true}, result.Value)
}

// failingResolveCredentials fails every resolve with resolveErr.
type failingResolveCredentials struct{ resolveErr error }

func (credentials failingResolveCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	return Credentials{}, credentials.resolveErr
}

func TestCreateOrUpdateContactClassifiesCredentialResolveFailures(t *testing.T) {
	input := CreateOrUpdateContactInput{Email: "member@meridian.example"}
	for name, test := range map[string]struct {
		credentials sdkgo.CredentialProvider[Credentials]
		isTransient bool
	}{
		"broker unavailable":      {credentials: failingResolveCredentials{projectconfig.ErrCredentialBrokerUnavailable}, isTransient: true},
		"not configured":          {credentials: sdkgo.StaticCredentialProvider[Credentials]{}, isTransient: true},
		"unlisted":                {credentials: failingResolveCredentials{errors.New("unlisted resolve failure")}, isTransient: true},
		"sdk reauthorization":     {credentials: failingResolveCredentials{sdkgo.NewReauthorizationRequiredError(nil)}},
		"project reauthorization": {credentials: failingResolveCredentials{projectconfig.ErrReauthorizationRequired}},
		"object not found":        {credentials: failingResolveCredentials{projectconfig.ErrObjectNotFound}},
		"not selected":            {credentials: failingResolveCredentials{projectconfig.ErrConnectionNotSelected}},
		"broker rejected":         {credentials: failingResolveCredentials{projectconfig.ErrCredentialBrokerRejected}},
		"blank key":               {credentials: sdkgo.StaticCredentialProvider[Credentials]{testConnection: {}}},
	} {
		client, err := New(Config{BaseURL: "http://127.0.0.1:1/v3"}, test.credentials)
		require.NoError(t, err)
		result, err := sdkgo.RunMutation(providerContext{}, client.CreateOrUpdateContact(), testConnection, input)
		if test.isTransient {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, name)
			require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind, name)
			continue
		}
		require.NoError(t, err, name)
		require.Equal(t, CreateOrUpdateContactBranchDefect, result.Branch, name)
	}
}

func TestCreateOrUpdateContactRejectsInvalidEmailBeforeDispatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider request") }))
	defer server.Close()
	result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).CreateOrUpdateContact(), testConnection, CreateOrUpdateContactInput{Email: "bad"})
	require.NoError(t, err)
	require.Equal(t, CreateOrUpdateContactBranchDefect, result.Branch)
}

func TestGetContactAndNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/v3/contacts/member@meridian.example", r.URL.Path)
		require.Equal(t, "brevo-test-key", r.Header.Get("api-key"))
		_, err := fmt.Fprint(w, `{"id":21,"email":"member@meridian.example"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(providerContext{}, newProviderClient(t, server.URL).GetContact(), testConnection, GetContactInput{Email: "member@meridian.example"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchFound, result.Branch)
	require.Equal(t, Contact{ID: 21, Email: "member@meridian.example"}, result.Value)

	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer missing.Close()
	result, err = sdkgo.RunQuery(providerContext{}, newProviderClient(t, missing.URL).GetContact(), testConnection, GetContactInput{Email: "member@meridian.example"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchNotFound, result.Branch)
}
