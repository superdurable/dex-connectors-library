// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestDecodeResolvedCredentialsRejectsRenewalMaterial(t *testing.T) {
	resolved, err := DecodeResolvedCredentialsJSON(json.RawMessage(`{"auth_method":"entra-app-only","access_token":"short-lived"}`))
	require.NoError(t, err)
	require.Equal(t, "short-lived", resolved.AccessToken.Reveal())
	for _, contents := range []string{
		`{"auth_method":"entra-app-only","access_token":"short-lived","client_secret":"long-lived"}`,
		`{"auth_method":"microsoft-oauth","access_token":"short-lived","refresh_token":"long-lived"}`,
		`{"auth_method":"another-method","access_token":"short-lived"}`,
		`{"auth_method":"entra-app-only","access_token":"has space"}`,
	} {
		_, err := DecodeResolvedCredentialsJSON(json.RawMessage(contents))
		require.Error(t, err, contents)
		require.NotContains(t, err.Error(), "long-lived")
	}
}

func TestEncodeCredentialsJSONRoundTripsCompleteCredentials(t *testing.T) {
	credentials := Credentials{
		AuthMethodID: MicrosoftOAuthAuthMethodID, ClientID: "11111111-2222-4333-8444-555555555555",
		ClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString("access"),
		RefreshToken: sdkgo.NewSecretString("refresh"),
	}
	encoded, err := EncodeCredentialsJSON(credentials)
	require.NoError(t, err)
	decoded, err := DecodeCredentialsJSON(encoded)
	require.NoError(t, err)
	require.Equal(t, "refresh", decoded.RefreshToken.Reveal())
	require.Equal(t, credentials.ClientID, decoded.ClientID)

	_, err = EncodeCredentialsJSON(Credentials{AuthMethodID: EntraAppOnlyAuthMethodID, ClientID: credentials.ClientID})
	require.Error(t, err, "an app-only credential needs its tenant and secret")
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, "client-secret")
	require.NotContains(t, rendered, "refresh\"")
}
