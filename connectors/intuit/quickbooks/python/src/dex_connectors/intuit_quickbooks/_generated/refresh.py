# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

# Validated manifest token grant declaration.
AUTHORIZATION = {'type': 'oauth2', 'fields': [{'name': 'client_id', 'type': 'string', 'required': True}, {'name': 'client_secret', 'type': 'secretString', 'required': True}, {'name': 'access_token', 'type': 'secretString', 'required': True}, {'name': 'refresh_token', 'type': 'secretString', 'required': True}, {'name': 'id_token', 'type': 'secretString', 'required': False}], 'oauth2': {'tokenEndpoint': 'https://oauth.platform.intuit.com/oauth2/v1/tokens/bearer', 'clientIDCredential': 'client_id', 'clientSecretCredential': 'client_secret', 'scopes': ['com.intuit.quickbooks.accounting', 'openid'], 'credentialMappings': [{'credential': 'access_token', 'source': 'access_token'}, {'credential': 'refresh_token', 'source': 'refresh_token'}, {'credential': 'id_token', 'source': 'id_token'}], 'tokenEndpointAuthMethod': 'client_secret_basic', 'refresh': {'scopeSeparators': 'whitespaceAndComma', 'requiredScopes': ['com.intuit.quickbooks.accounting'], 'terminalErrorCodes': ['invalid_grant', 'invalid_client', 'unauthorized_client', 'unsupported_grant_type'], 'missingExpiry': 'reject', 'storedMissingExpiry': 'refresh', 'grants': [{'refreshTokenCredential': 'refresh_token', 'credentialMappings': [{'credential': 'access_token', 'source': 'access_token'}, {'credential': 'refresh_token', 'source': 'refresh_token'}]}]}}, 'refreshable': True}

# Stored credential names mapped to runtime dataclass fields.
CREDENTIAL_FIELDS = {'client_id': 'client_id', 'client_secret': 'client_secret', 'access_token': 'access_token', 'refresh_token': 'refresh_token', 'id_token': 'id_token'}
