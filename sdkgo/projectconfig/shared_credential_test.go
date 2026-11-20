// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// hostKeptToken is a connector credential whose host keeps the refresh token, as a generated credential type is.
type hostKeptToken struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
}

// UnmarshalSharedCredential requires the access token but not the host-kept refresh token.
func (value *hostKeptToken) UnmarshalSharedCredential(contents []byte) error {
	var decoded hostKeptToken
	if err := projectconfig.DecodeCredentials(contents, &decoded); err != nil {
		return err
	}
	if decoded.Access == "" {
		return errors.New("credential access is required")
	}
	*value = decoded
	return nil
}

// decodeCompleteHostKeptToken is the connector's ordinary decoder, which requires every field.
func decodeCompleteHostKeptToken(contents json.RawMessage) (hostKeptToken, error) {
	var value hostKeptToken
	if err := projectconfig.DecodeCredentials(contents, &value); err != nil {
		return hostKeptToken{}, err
	}
	if value.Access == "" || value.Refresh == "" {
		return hostKeptToken{}, errors.New("credential access and refresh are required")
	}
	return value, nil
}

// decodeCompleteToken is the decoder of a connector version without UnmarshalSharedCredential.
func decodeCompleteToken(contents json.RawMessage) (token, error) {
	value, err := decodeCompleteHostKeptToken(contents)
	return token(value), err
}

// strippedReply is a READY answer whose credential omits the host-kept refresh token.
func strippedReply(revision uint64, credential map[string]string) brokerReply {
	return brokerReply{body: map[string]any{
		"status": "READY", "credential": credential, "revision": revision,
		"issuedAt": epoch.Format(time.RFC3339Nano), "expiresAt": epoch.Add(time.Hour).Format(time.RFC3339Nano),
	}}
}

func TestSharedEntryDecodesACredentialWithoutHostKeptSecrets(t *testing.T) {
	for name, credential := range map[string]map[string]string{
		"without the host-kept refresh token":               {"access": sharedAccessOne},
		"with it, from a broker that does not strip it yet": {"access": sharedAccessOne, "refresh": "refresh-secret"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newUnreadSharedFixture(t, sharedScope, strippedReply(1, credential))
			forbiddenDecoder := func(json.RawMessage) (hostKeptToken, error) {
				t.Error("a shared entry must decode with UnmarshalSharedCredential, not with the decoder")
				return hostKeptToken{}, errors.New("forbidden decoder")
			}
			resolver, err := projectconfig.NewCredentialResolver(fixture.project.Connections, sharedKey, forbiddenDecoder, nil)
			require.NoError(t, err)
			value, err := resolver.Resolve(context.Background())
			require.NoError(t, err)
			require.Equal(t, hostKeptToken{Access: sharedAccessOne, Refresh: credential["refresh"]}, value)

			apiKey, err := provider.NewCredentialProvider(fixture.project.Connections, sharedKey, forbiddenDecoder)
			require.NoError(t, err)
			value, err = apiKey.Resolve(sharedCall)
			require.NoError(t, err)
			require.Equal(t, sharedAccessOne, value.Access)
		})
	}
}

func TestSharedCredentialThatFailsUnmarshalSharedCredentialIsRejectedWithoutSecrets(t *testing.T) {
	fixture := newUnreadSharedFixture(t, sharedScope, strippedReply(1, map[string]string{"refresh": "refresh-secret"}))
	resolver, err := projectconfig.NewCredentialResolver(fixture.project.Connections, sharedKey, decodeCompleteHostKeptToken, nil)
	require.NoError(t, err)

	_, err = resolver.Resolve(context.Background())
	require.EqualError(t, err, "shared project credential decoding failed")
	require.ErrorIs(t, err, projectconfig.ErrCredentialMaterialInvalid)
	requireNoSecrets(t, err)
}

func TestConnectorWithoutUnmarshalSharedCredentialNeedsTheCompleteSharedCredential(t *testing.T) {
	fixture := newUnreadSharedFixture(t, sharedScope, strippedReply(1, map[string]string{"access": sharedAccessOne}))
	resolver, err := projectconfig.NewCredentialResolver(fixture.project.Connections, sharedKey, decodeCompleteToken, nil)
	require.NoError(t, err)
	_, err = resolver.Resolve(context.Background())
	require.EqualError(t, err, "project credential decoding failed: this connector version does not support shared credentials")
	require.ErrorIs(t, err, projectconfig.ErrCredentialMaterialInvalid)
	requireNoSecrets(t, err)

	fixture = newUnreadSharedFixture(t, sharedScope, readyReply(1, sharedAccessOne, epoch, time.Hour))
	resolver, err = projectconfig.NewCredentialResolver(fixture.project.Connections, sharedKey, decodeCompleteToken, nil)
	require.NoError(t, err)
	value, err := resolver.Resolve(context.Background())
	require.NoError(t, err)
	require.Equal(t, token{Access: sharedAccessOne, Refresh: "refresh-secret"}, value, "a broker that keeps nothing serves older connectors")
}

func TestOrdinaryEntryDecodesWithTheDecoderEvenWhenTheTypeDecodesSharedCredentials(t *testing.T) {
	fixture := newSharedFixture(t, readyReply(1, sharedAccessOne, epoch, time.Hour))
	fixture.allowsStoredConnections = true
	store, err := projectconfig.NewConnectionStore(&projectconfig.ConnectionStoreConfig{Objects: fixture.objects, Scope: sharedScope})
	require.NoError(t, err)
	expiresAt := time.Now().Add(time.Hour)
	_, err = store.ReplaceCredential(context.Background(), ordinaryKey, 0, projectconfig.CredentialMaterial{Credentials: []byte(`{"access":"stored"}`), ExpiresAt: &expiresAt, AuthMethod: "default"})
	require.NoError(t, err)

	resolver, err := projectconfig.NewCredentialResolver(fixture.project.Connections, ordinaryKey, decodeCompleteHostKeptToken, nil)
	require.NoError(t, err)
	_, err = resolver.Resolve(context.Background())
	require.EqualError(t, err, "project credential decoding failed", "a stored credential keeps every field, so its decoder requires them")
	require.ErrorIs(t, err, projectconfig.ErrCredentialMaterialInvalid)
}
