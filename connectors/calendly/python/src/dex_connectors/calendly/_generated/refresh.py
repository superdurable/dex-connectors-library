# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

# Validated manifest token grant declaration.
AUTHORIZATION = {'refreshable': True, 'defaultMethod': 'personal-access-token', 'methods': [{'id': 'personal-access-token', 'type': 'apiKey', 'fields': [{'name': 'access_token', 'type': 'secretString', 'required': True}, {'name': 'webhook_signing_key', 'type': 'secretString', 'required': False}]}, {'id': 'calendly-oauth', 'type': 'oauth2', 'fields': [{'name': 'oauth_client_id', 'type': 'string', 'required': True}, {'name': 'oauth_client_secret', 'type': 'secretString', 'required': True}, {'name': 'access_token', 'type': 'secretString', 'required': True}, {'name': 'refresh_token', 'type': 'secretString', 'required': True}, {'name': 'webhook_signing_key', 'type': 'secretString', 'required': False}], 'oauth2': {'tokenEndpoint': 'https://auth.calendly.com/oauth/token', 'clientIDCredential': 'oauth_client_id', 'clientSecretCredential': 'oauth_client_secret', 'scopes': ['users:read', 'event_types:read', 'scheduled_events:write', 'scheduling_links:write', 'webhooks:write'], 'credentialMappings': [{'credential': 'access_token', 'source': 'access_token'}, {'credential': 'refresh_token', 'source': 'refresh_token'}], 'tokenEndpointAuthMethod': 'client_secret_basic', 'refresh': {'responseScopes': 'ignore', 'terminalErrorCodes': ['invalid_grant', 'invalid_client', 'unauthorized_client'], 'missingExpiry': 'reject', 'storedMissingExpiry': 'refresh'}}}]}

# Stored credential names mapped to runtime dataclass fields.
CREDENTIAL_FIELDS = {'access_token': 'access_token', 'webhook_signing_key': 'webhook_signing_key', 'oauth_client_id': 'oauth_client_id', 'oauth_client_secret': 'oauth_client_secret', 'refresh_token': 'refresh_token'}
