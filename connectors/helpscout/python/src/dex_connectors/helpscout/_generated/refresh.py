# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

# Validated manifest token grant declaration.
AUTHORIZATION = {'type': 'apiKey', 'fields': [{'name': 'app_id', 'type': 'string', 'required': True}, {'name': 'app_secret', 'type': 'secretString', 'required': True}, {'name': 'access_token', 'type': 'secretString', 'required': False}, {'name': 'webhook_secret', 'type': 'secretString', 'required': False}], 'clientCredentials': {'tokenExchangeTimeout': '25s', 'tokenEndpoint': 'https://api.helpscout.net/v2/oauth2/token', 'clientIDCredential': 'app_id', 'clientSecretCredential': 'app_secret', 'credentialMappings': [{'credential': 'access_token', 'source': 'access_token'}], 'terminalErrorCodes': ['invalid_client', 'unauthorized_client'], 'missingExpiry': 'reject', 'responseScopes': 'ignore'}, 'refreshable': True}

# Stored credential names mapped to runtime dataclass fields.
CREDENTIAL_FIELDS = {'app_id': 'app_id', 'app_secret': 'app_secret', 'access_token': 'access_token', 'webhook_secret': 'webhook_secret'}
