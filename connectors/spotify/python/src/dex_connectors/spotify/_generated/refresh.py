# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

# Validated manifest token grant declaration.
AUTHORIZATION = {'type': 'oauth2', 'fields': [{'name': 'oauth_client_id', 'type': 'string', 'required': False}, {'name': 'oauth_client_secret', 'type': 'secretString', 'required': False}, {'name': 'access_token', 'type': 'secretString', 'required': True}, {'name': 'refresh_token', 'type': 'secretString', 'required': False}], 'oauth2': {'tokenEndpoint': 'https://accounts.spotify.com/api/token', 'clientIDCredential': 'oauth_client_id', 'clientSecretCredential': 'oauth_client_secret', 'scopes': ['playlist-read-private'], 'credentialMappings': [{'credential': 'access_token', 'source': 'access_token'}, {'credential': 'refresh_token', 'source': 'refresh_token'}], 'tokenEndpointAuthMethod': 'client_secret_basic', 'refresh': {'exactScopes': True, 'scopeSeparators': 'whitespaceAndComma', 'allowsPublicClient': True, 'terminalErrorCodes': ['invalid_grant', 'invalid_client', 'unauthorized_client', 'access_denied']}}, 'refreshable': True}

# Stored credential names mapped to runtime dataclass fields.
CREDENTIAL_FIELDS = {'oauth_client_id': 'oauth_client_id', 'oauth_client_secret': 'oauth_client_secret', 'access_token': 'access_token', 'refresh_token': 'refresh_token'}
