# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

# Validated manifest token grant declaration.
AUTHORIZATION = {'refreshable': True, 'defaultMethod': 'microsoft-oauth', 'methods': [{'id': 'microsoft-oauth', 'type': 'oauth2', 'fields': [{'name': 'oauth_client_id', 'type': 'string', 'required': True}, {'name': 'oauth_client_secret', 'type': 'secretString', 'required': True}, {'name': 'access_token', 'type': 'secretString', 'required': True}, {'name': 'refresh_token', 'type': 'secretString', 'required': True}], 'oauth2': {'tokenEndpoint': 'https://login.microsoftonline.com/organizations/oauth2/v2.0/token', 'clientIDCredential': 'oauth_client_id', 'clientSecretCredential': 'oauth_client_secret', 'scopes': ['offline_access', 'Files.ReadWrite.All'], 'credentialMappings': [{'credential': 'access_token', 'source': 'access_token'}, {'credential': 'refresh_token', 'source': 'refresh_token'}], 'refresh': {'scopeResourcePrefix': 'https://graph.microsoft.com/', 'scopeResourcePrefixCaseInsensitive': True, 'caseInsensitiveScopes': True, 'scopeSeparators': 'whitespaceAndComma', 'terminalErrorCodes': ['invalid_grant', 'invalid_client', 'unauthorized_client', 'interaction_required', 'consent_required', 'invalid_scope', 'invalid_request', 'unsupported_grant_type', 'invalid_resource'], 'missingExpiry': 'reject', 'storedMissingExpiry': 'refresh', 'additionalParameters': {'scope': 'offline_access Files.ReadWrite.All'}}}}]}

# Stored credential names mapped to runtime dataclass fields.
CREDENTIAL_FIELDS = {'oauth_client_id': 'oauth_client_id', 'oauth_client_secret': 'oauth_client_secret', 'access_token': 'access_token', 'refresh_token': 'refresh_token'}
