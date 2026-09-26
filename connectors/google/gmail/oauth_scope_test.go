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
			OAuth2 struct {
				Scopes     []string `yaml:"scopes"`
				UserScopes []string `yaml:"userScopes"`
			} `yaml:"oauth2"`
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
	oauth := manifest.Spec.Auth.OAuth2
	require.Empty(t, oauth.UserScopes)
	for _, scope := range oauth.Scopes {
		isCanonicalGoogleScope := scope == "openid" ||
			(strings.HasPrefix(scope, googleScopeURIPrefix) && len(scope) > len(googleScopeURIPrefix))
		require.Truef(t, isCanonicalGoogleScope, "scope %q is neither openid nor a canonical Google scope URI", scope)
	}
	require.Contains(t, oauth.Scopes, "openid")
	require.Contains(t, oauth.Scopes, googleScopeURIPrefix+"userinfo.email")
}
