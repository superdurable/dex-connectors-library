# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

# Validated manifest token grant declaration.
AUTHORIZATION = {'type': 'apiKey', 'fields': [{'name': 'client_id', 'type': 'string', 'required': True}, {'name': 'client_secret', 'type': 'secretString', 'required': True}, {'name': 'access_token', 'type': 'secretString', 'required': False}], 'clientCredentials': {'tokenExchangeTimeout': '15s', 'tokenEndpoint': 'https://api-sandbox.xiaoman.cn/v1/oauth2/access_token', 'tokenEndpointAuthMethod': 'client_secret_post', 'clientIDCredential': 'client_id', 'clientSecretCredential': 'client_secret', 'scopes': ['company'], 'credentialMappings': [{'credential': 'access_token', 'source': 'access_token'}], 'terminalErrorCodes': ['invalid_client', 'invalid_scope'], 'missingExpiry': {'lifetime': '8h'}, 'responseScopes': 'ignore', 'storedMissingExpiry': 'refresh'}, 'refreshable': True}

# Stored credential names mapped to runtime dataclass fields.
CREDENTIAL_FIELDS = {'client_id': 'client_id', 'client_secret': 'client_secret', 'access_token': 'access_token'}
