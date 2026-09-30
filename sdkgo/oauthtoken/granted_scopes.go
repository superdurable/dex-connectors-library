// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauthtoken

import "strings"

// HasAllScopes reports whether a returned scope value grants every required
// scope. Scopes are separated by spaces (RFC 6749 section 3.3) or commas, which
// GitHub and Slack return. Extra granted scopes are allowed.
func HasAllScopes(returnedScope string, requiredScopes []string) bool {
	grantedScopes := splitGrantedScopes(returnedScope)
	for _, requiredScope := range requiredScopes {
		if !grantedScopes[requiredScope] {
			return false
		}
	}
	return true
}

// HasExactScopes reports whether a returned scope value grants exactly the
// required scopes as a set, with the separators HasAllScopes accepts.
func HasExactScopes(returnedScope string, requiredScopes []string) bool {
	grantedScopes := splitGrantedScopes(returnedScope)
	requiredScopeSet := make(map[string]bool, len(requiredScopes))
	for _, requiredScope := range requiredScopes {
		requiredScopeSet[requiredScope] = true
	}
	return len(grantedScopes) == len(requiredScopeSet) && HasAllScopes(returnedScope, requiredScopes)
}

func splitGrantedScopes(returnedScope string) map[string]bool {
	grantedScopes := map[string]bool{}
	for _, scope := range strings.FieldsFunc(returnedScope, func(character rune) bool {
		return character == ' ' || character == ','
	}) {
		grantedScopes[scope] = true
	}
	return grantedScopes
}
