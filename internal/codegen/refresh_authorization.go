// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"encoding/json"
	"fmt"

	"github.com/superdurable/dex-connectors-library/schema"
)

// GenerateRefreshAuthorization encodes the manifest grant settings consumed by the language SDK refresh drivers.
func GenerateRefreshAuthorization(manifest schema.Manifest) ([]byte, error) {
	authorizationJSON, err := json.Marshal(manifest.Spec.Auth)
	if err != nil {
		return nil, fmt.Errorf("encode refresh authorization: %w", err)
	}
	var authorizationFields map[string]any
	if err := json.Unmarshal(authorizationJSON, &authorizationFields); err != nil {
		return nil, fmt.Errorf("decode refresh authorization: %w", err)
	}
	trimRefreshMetadata(authorizationFields)
	if len(manifest.Spec.Auth.Methods) > 0 {
		delete(authorizationFields, "fields")
	}
	authorization, err := json.MarshalIndent(authorizationFields, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode refresh authorization: %w", err)
	}
	return authorization, nil
}

func trimRefreshMetadata(value any) {
	switch fields := value.(type) {
	case map[string]any:
		for _, name := range []string{"description", "displayName", "goName", "guide", "connectionKind", "recommended", "methodLabel", "configuration", "authorizationEndpoint", "authorizationParameters", "credentialDerivations", "pkce", "oidc", "protocol"} {
			delete(fields, name)
		}
		for name, field := range fields {
			switch name {
			case "methods", "fields", "oauth2", "clientCredentials", "serviceAccount", "refresh", "grants", "jwtBearer":
				trimRefreshMetadata(field)
			}
		}
	case []any:
		for _, item := range fields {
			trimRefreshMetadata(item)
		}
	}
}
