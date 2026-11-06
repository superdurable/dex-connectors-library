# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

# Validated manifest token grant declaration.
AUTHORIZATION = {'refreshable': True, 'defaultMethod': 'personal-api-token', 'methods': [{'id': 'personal-api-token', 'type': 'apiKey', 'fields': [{'name': 'api_token', 'type': 'secretString', 'required': True}]}, {'id': 'monday-oauth', 'type': 'oauth2', 'fields': [{'name': 'oauth_client_id', 'type': 'string', 'required': True}, {'name': 'oauth_client_secret', 'type': 'secretString', 'required': True}, {'name': 'access_token', 'type': 'secretString', 'required': True}, {'name': 'refresh_token', 'type': 'secretString', 'required': True}], 'oauth2': {'tokenEndpoint': 'https://auth.monday.com/oauth_ms/oauth/token', 'clientIDCredential': 'oauth_client_id', 'clientSecretCredential': 'oauth_client_secret', 'scopes': ['boards:read', 'boards:write', 'updates:write'], 'credentialMappings': [{'credential': 'access_token', 'source': 'access_token'}, {'credential': 'refresh_token', 'source': 'refresh_token'}], 'tokenRequestEncoding': 'json', 'refresh': {'accessTokenJWTExpiryFallback': '1h', 'scopeSeparators': 'whitespaceAndComma', 'terminalErrorCodes': ['invalid_grant', 'invalid_client', 'unauthorized_client', 'invalid_token'], 'missingExpiry': 'keep', 'storedMissingExpiry': 'refresh'}}}]}

# Stored credential names mapped to runtime dataclass fields.
CREDENTIAL_FIELDS = {'api_token': 'api_token', 'oauth_client_id': 'oauth_client_id', 'oauth_client_secret': 'oauth_client_secret', 'access_token': 'access_token', 'refresh_token': 'refresh_token'}
