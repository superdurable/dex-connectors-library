// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const googleScopeURIPrefix = "https://www.googleapis.com/auth/"

type gmailOAuthManifest struct {
	Spec struct {
		Provider string `yaml:"provider"`
		Auth     struct {
			Methods []struct {
				ID     string `yaml:"id"`
				OAuth2 struct {
					Scopes     []string `yaml:"scopes"`
					UserScopes []string `yaml:"userScopes"`
				} `yaml:"oauth2"`
			} `yaml:"methods"`
		} `yaml:"auth"`
	} `yaml:"spec"`
}

// Google reports alias grants such as email under canonical URIs, and Dex Web matches granted scopes literally.
func TestOAuthScopesUseCanonicalGoogleNames(t *testing.T) {
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest gmailOAuthManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	require.Equal(t, "google", manifest.Spec.Provider)
	var oauthScopes []string
	var userScopes []string
	for _, method := range manifest.Spec.Auth.Methods {
		if method.ID == "google-oauth" {
			oauthScopes = method.OAuth2.Scopes
			userScopes = method.OAuth2.UserScopes
		}
	}
	require.Empty(t, userScopes)
	for _, scope := range oauthScopes {
		isCanonicalGoogleScope := scope == "openid" ||
			(strings.HasPrefix(scope, googleScopeURIPrefix) && len(scope) > len(googleScopeURIPrefix))
		require.Truef(t, isCanonicalGoogleScope, "scope %q is neither openid nor a canonical Google scope URI", scope)
	}
	require.Contains(t, oauthScopes, "openid")
	require.Contains(t, oauthScopes, googleScopeURIPrefix+"userinfo.email")
}
