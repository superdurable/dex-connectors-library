// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

type token struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
}

func decodeToken(contents json.RawMessage) (token, error) {
	var value token
	return value, json.Unmarshal(contents, &value)
}

func encodeToken(value token) (json.RawMessage, error) { return json.Marshal(value) }

var refreshKey = projectconfig.ConnectionKey{ConnectorID: "github", ConnectionName: "releases"}

// expiredConnection stores a READY credential whose access token expired an hour ago.
func expiredConnection(t *testing.T) *projectconfig.ConnectionStore {
	t.Helper()
	store, err := projectconfig.NewConnectionStore(&projectconfig.ConnectionStoreConfig{Objects: testsupport.NewObjectStore(), Scope: inboxScope})
	require.NoError(t, err)
	expired := time.Now().Add(-time.Hour)
	material := projectconfig.CredentialMaterial{Credentials: json.RawMessage(`{"access":"old","refresh":"r1"}`), ExpiresAt: &expired, AuthMethod: "default"}
	_, err = store.ReplaceCredential(context.Background(), refreshKey, 0, material)
	require.NoError(t, err)
	return store
}

func currentToken(t *testing.T, store *projectconfig.ConnectionStore) (token, projectconfig.Connection) {
	t.Helper()
	material, connection, err := store.ReadCredentialMaterial(context.Background(), refreshKey)
	require.NoError(t, err)
	value, err := decodeToken(material.Credentials)
	require.NoError(t, err)
	return value, connection
}

func TestRefreshPublishesTheReplacementCredential(t *testing.T) {
	store := expiredConnection(t)
	resolver, err := projectconfig.NewCredentialResolver(store, refreshKey, decodeToken, encodeToken)
	require.NoError(t, err)
	expires := time.Now().Add(time.Hour)
	value, err := resolver.ResolveWithRefresh(context.Background(), func(_ context.Context, state projectconfig.RefreshState[token]) (projectconfig.RefreshResult[token], error) {
		require.Equal(t, "r1", state.Credentials.Refresh)
		return projectconfig.RefreshResult[token]{Credentials: token{Access: "new", Refresh: "r2"}, ExpiresAt: expires}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "new", value.Access)
	stored, connection := currentToken(t, store)
	require.Equal(t, token{Access: "new", Refresh: "r2"}, stored)
	require.Equal(t, projectconfig.CredentialReady, connection.Status)
}

func TestTransientRefreshFailureKeepsTheCredential(t *testing.T) {
	store := expiredConnection(t)
	resolver, err := projectconfig.NewCredentialResolver(store, refreshKey, decodeToken, encodeToken)
	require.NoError(t, err)
	unavailable := errors.New("token endpoint returned 503")
	_, err = resolver.ResolveWithRefresh(context.Background(), func(context.Context, projectconfig.RefreshState[token]) (projectconfig.RefreshResult[token], error) {
		return projectconfig.RefreshResult[token]{}, unavailable
	})
	require.ErrorIs(t, err, projectconfig.ErrRefreshFailed)
	require.ErrorIs(t, err, unavailable)
	require.NotErrorIs(t, err, projectconfig.ErrReauthorizationRequired)
	stored, connection := currentToken(t, store)
	require.Equal(t, token{Access: "old", Refresh: "r1"}, stored)
	require.Equal(t, projectconfig.CredentialReady, connection.Status)

	// The next call refreshes again instead of requiring a person to authorize.
	_, err = resolver.ResolveWithRefresh(context.Background(), func(context.Context, projectconfig.RefreshState[token]) (projectconfig.RefreshResult[token], error) {
		return projectconfig.RefreshResult[token]{Credentials: token{Access: "new", Refresh: "r1"}, ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	require.NoError(t, err)
}

func TestRejectedRefreshTokenRequiresReauthorization(t *testing.T) {
	store := expiredConnection(t)
	resolver, err := projectconfig.NewCredentialResolver(store, refreshKey, decodeToken, encodeToken)
	require.NoError(t, err)
	_, err = resolver.ResolveWithRefresh(context.Background(), func(context.Context, projectconfig.RefreshState[token]) (projectconfig.RefreshResult[token], error) {
		return projectconfig.RefreshResult[token]{}, fmt.Errorf("%w: invalid_grant", projectconfig.ErrReauthorizationRequired)
	})
	require.ErrorIs(t, err, projectconfig.ErrReauthorizationRequired)
	connection, err := store.ReadConnection(context.Background(), refreshKey)
	require.NoError(t, err)
	require.Equal(t, projectconfig.CredentialReauthorizationRequired, connection.Status)
}

func TestNonRefreshingResolverRefusesToRefresh(t *testing.T) {
	store := expiredConnection(t)
	resolver, err := projectconfig.NewCredentialResolver[token](store, refreshKey, decodeToken, nil)
	require.NoError(t, err)
	_, err = resolver.ResolveWithRefresh(context.Background(), func(context.Context, projectconfig.RefreshState[token]) (projectconfig.RefreshResult[token], error) {
		t.Fatal("a non-refreshing resolver must not call the provider")
		return projectconfig.RefreshResult[token]{}, nil
	})
	require.Error(t, err)
}
