# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

# Validated manifest token grant declaration.
AUTHORIZATION = {'refreshable': True, 'defaultMethod': 'jwt-bearer', 'methods': [{'id': 'jwt-bearer', 'type': 'serviceAccount', 'fields': [{'name': 'client_id', 'type': 'string', 'required': True}, {'name': 'username', 'type': 'string', 'required': True}, {'name': 'private_key', 'type': 'secretString', 'required': True}, {'name': 'login_environment', 'type': 'enum', 'required': False, 'enum': ['production', 'sandbox']}, {'name': 'access_token', 'type': 'secretString', 'required': False}, {'name': 'instance_url', 'type': 'string', 'required': False}], 'serviceAccount': {'jwtBearer': {'privateKey': {'fromCredential': 'private_key'}, 'issuer': {'fromCredential': 'client_id'}, 'subject': {'fromCredential': 'username'}, 'audience': {'fromCredential': 'login_environment', 'values': {'': 'https://login.example.com', 'production': 'https://login.example.com', 'sandbox': 'https://test.example.com'}}, 'tokenEndpoint': {'fromCredential': 'login_environment', 'values': {'': 'https://login.example.com/token', 'production': 'https://login.example.com/token', 'sandbox': 'https://test.example.com/token'}}, 'lifetime': '2m', 'credentialMappings': [{'credential': 'access_token', 'source': 'access_token'}, {'credential': 'instance_url', 'source': 'instance_url'}], 'terminalErrorCodes': ['invalid_grant', 'invalid_client'], 'missingExpiry': {'lifetime': '2h'}}}}]}

# Stored credential names mapped to runtime dataclass fields.
CREDENTIAL_FIELDS = {'client_id': 'client_id', 'username': 'username', 'private_key': 'private_key', 'login_environment': 'login_environment', 'access_token': 'access_token', 'instance_url': 'instance_url'}
