# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

# Validated manifest token grant declaration.
AUTHORIZATION = {'type': 'oauth2', 'fields': [{'name': 'oauth_client_id', 'type': 'string', 'required': True}, {'name': 'oauth_client_secret', 'type': 'secretString', 'required': True}, {'name': 'developer_token', 'type': 'secretString', 'required': True}, {'name': 'access_token', 'type': 'secretString', 'required': True}, {'name': 'refresh_token', 'type': 'secretString', 'required': True}], 'oauth2': {'tokenEndpoint': 'https://login.microsoftonline.com/common/oauth2/v2.0/token', 'clientIDCredential': 'oauth_client_id', 'clientSecretCredential': 'oauth_client_secret', 'scopes': ['offline_access', 'https://ads.microsoft.com/msads.manage'], 'credentialMappings': [{'credential': 'access_token', 'source': 'access_token'}, {'credential': 'refresh_token', 'source': 'refresh_token'}], 'tokenEndpointAuthMethod': 'client_secret_post', 'refresh': {'caseInsensitiveScopes': True, 'tokenExchangeTimeout': '15s', 'terminalErrorCodes': ['invalid_grant', 'invalid_client', 'unauthorized_client', 'interaction_required', 'consent_required'], 'missingExpiry': 'reject', 'storedMissingExpiry': 'refresh'}}, 'refreshable': True}

# Stored credential names mapped to runtime dataclass fields.
CREDENTIAL_FIELDS = {'oauth_client_id': 'oauth_client_id', 'oauth_client_secret': 'oauth_client_secret', 'developer_token': 'developer_token', 'access_token': 'access_token', 'refresh_token': 'refresh_token'}
