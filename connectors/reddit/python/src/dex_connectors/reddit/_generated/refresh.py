# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

# Validated manifest token grant declaration.
AUTHORIZATION = {'type': 'oauth2', 'fields': [{'name': 'oauth_client_id', 'type': 'string', 'required': True}, {'name': 'oauth_client_secret', 'type': 'secretString', 'required': True}, {'name': 'access_token', 'type': 'secretString', 'required': True}, {'name': 'refresh_token', 'type': 'secretString', 'required': True}], 'oauth2': {'tokenEndpoint': 'https://www.reddit.com/api/v1/access_token', 'clientIDCredential': 'oauth_client_id', 'clientSecretCredential': 'oauth_client_secret', 'scopes': ['read'], 'credentialMappings': [{'credential': 'access_token', 'source': 'access_token'}, {'credential': 'refresh_token', 'source': 'refresh_token'}], 'tokenEndpointAuthMethod': 'client_secret_basic', 'refresh': {'tokenExchangeTimeout': '15s', 'scopeSeparators': 'whitespaceAndComma', 'requiredScopes': ['read'], 'terminalErrorCodes': ['invalid_grant', 'invalid_client', 'unauthorized_client'], 'missingExpiry': 'reject', 'storedMissingExpiry': 'refresh'}}, 'refreshable': True}

# Stored credential names mapped to runtime dataclass fields.
CREDENTIAL_FIELDS = {'oauth_client_id': 'oauth_client_id', 'oauth_client_secret': 'oauth_client_secret', 'access_token': 'access_token', 'refresh_token': 'refresh_token'}
