// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauthtoken

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrantedScopeChecks(t *testing.T) {
	for _, test := range []struct {
		name           string
		returnedScope  string
		requiredScopes []string
		hasAllScopes   bool
		hasExactScopes bool
	}{
		{name: "space separated exact", returnedScope: "openid profile email", requiredScopes: []string{"email", "openid", "profile"}, hasAllScopes: true, hasExactScopes: true},
		{name: "comma separated exact", returnedScope: "channels:read,chat:write", requiredScopes: []string{"chat:write", "channels:read"}, hasAllScopes: true, hasExactScopes: true},
		{name: "mixed separators", returnedScope: "read:user, user:email", requiredScopes: []string{"read:user", "user:email"}, hasAllScopes: true, hasExactScopes: true},
		{name: "extra granted scope", returnedScope: "a b c", requiredScopes: []string{"a", "b"}, hasAllScopes: true},
		{name: "missing scope", returnedScope: "a", requiredScopes: []string{"a", "b"}},
		{name: "empty returned scope", returnedScope: "", requiredScopes: []string{"a"}},
		{name: "duplicate required scope", returnedScope: "a", requiredScopes: []string{"a", "a"}, hasAllScopes: true, hasExactScopes: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.hasAllScopes, HasAllScopes(test.returnedScope, test.requiredScopes))
			require.Equal(t, test.hasExactScopes, HasExactScopes(test.returnedScope, test.requiredScopes))
		})
	}
}
