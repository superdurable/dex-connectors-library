# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Call, SecretString, CredentialRefreshDriver
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver
from dex_connectors.sdk.providerhttp import new_provider_http_client

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, SEND_REQUEST_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import SendRequestInput, SendRequestOutput

class Connection:

    __slots__ = ('_send_request', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        send_request: Mutation[SendRequestInput, SendRequestOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or send_request is None:
            raise ValueError("OAuth 2.0 HTTP connection provider and operations are required")
        if send_request.definition().operation != SEND_REQUEST_DEFINITION.operation:
            raise ValueError("OAuth 2.0 HTTP connection operation identity differs")
        self.reference = reference
        self._send_request = send_request
        self._client = client

    def send_request(self) -> Mutation[SendRequestInput, SendRequestOutput]:
        return self._send_request

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "superdurable_oauth2_http.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("OAuth 2.0 HTTP connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.superdurable_oauth2_http.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("OAuth 2.0 HTTP client is required")
    return Connection(reference, client.send_request(), client)

setattr(new_connection, '__module__', 'dex_connectors.superdurable_oauth2_http.connection')


def new_connection_with_operations(
    send_request: Mutation[SendRequestInput, SendRequestOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, send_request)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.superdurable_oauth2_http.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("OAuth 2.0 HTTP stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token')):
        raise ValueError("OAuth 2.0 HTTP stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("OAuth 2.0 HTTP credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("OAuth 2.0 HTTP credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("OAuth 2.0 HTTP credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("OAuth 2.0 HTTP credential refresh_token has an invalid type")
    if not oauth_client_id:
        raise ValueError("OAuth 2.0 HTTP credential oauth_client_id is required")
    if not oauth_client_secret and not is_shared:
        raise ValueError("OAuth 2.0 HTTP credential oauth_client_secret is required")
    if not access_token:
        raise ValueError("OAuth 2.0 HTTP credential access_token is required")
    return Credentials(oauth_client_id, SecretString(oauth_client_secret or ''), SecretString(access_token), SecretString(refresh_token or ''))


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    if credentials.oauth_client_id:
        material["oauth_client_id"] = credentials.oauth_client_id
    if credentials.oauth_client_secret.reveal():
        material["oauth_client_secret"] = credentials.oauth_client_secret.reveal()
    if credentials.access_token.reveal():
        material["access_token"] = credentials.access_token.reveal()
    if credentials.refresh_token.reveal():
        material["refresh_token"] = credentials.refresh_token.reveal()
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("OAuth 2.0 HTTP", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

    async def resolve(self, call: Call) -> Credentials:
        return await self.provider.resolve_with_refresh(call, self.driver)

    async def resolve_with_refresh(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_with_refresh(call, driver or self.driver)

    async def resolve_after_rejection(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_after_rejection(call, driver or self.driver)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("OAuth 2.0 HTTP connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("OAuth 2.0 HTTP project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("OAuth 2.0 HTTP project connection provider differs")
    config = _decode_config(matching[0].configuration)
    if http_client is not None:
        raise ValueError("OAuth 2.0 HTTP private token endpoint does not accept a custom HTTP client")
    http_client = new_provider_http_client(30, address_policy="privateNetworkHTTP")
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.superdurable_oauth2_http.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"authorizationEndpoint", "tokenEndpoint", "scopes", "tokenEndpointAuthMethod", "apiBaseUrl", "maxBodyBytes", "requestTimeout"}:
        raise ValueError("OAuth 2.0 HTTP configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "authorizationEndpoint" in value:
        authorization_endpoint = value["authorizationEndpoint"]
        if not (isinstance(authorization_endpoint, str) and bool(authorization_endpoint)):
            raise ValueError("OAuth 2.0 HTTP configuration authorizationEndpoint has an invalid type")
        config_arguments['authorization_endpoint'] = authorization_endpoint
    else:
        raise ValueError("OAuth 2.0 HTTP configuration authorizationEndpoint is required")
    if "tokenEndpoint" in value:
        token_endpoint = value["tokenEndpoint"]
        if not (isinstance(token_endpoint, str) and bool(token_endpoint)):
            raise ValueError("OAuth 2.0 HTTP configuration tokenEndpoint has an invalid type")
        config_arguments['token_endpoint'] = token_endpoint
    else:
        raise ValueError("OAuth 2.0 HTTP configuration tokenEndpoint is required")
    if "scopes" in value:
        scopes = value["scopes"]
        if not (isinstance(scopes, list) and all(isinstance(item, str) for item in scopes)):
            raise ValueError("OAuth 2.0 HTTP configuration scopes has an invalid type")
        config_arguments['scopes'] = tuple(scopes)
    else:
        raise ValueError("OAuth 2.0 HTTP configuration scopes is required")
    if "tokenEndpointAuthMethod" in value and value["tokenEndpointAuthMethod"] != '':
        token_endpoint_auth_method = value["tokenEndpointAuthMethod"]
        if not (isinstance(token_endpoint_auth_method, str) and token_endpoint_auth_method in ('client_secret_post', 'client_secret_basic')):
            raise ValueError("OAuth 2.0 HTTP configuration tokenEndpointAuthMethod has an invalid type")
        config_arguments['token_endpoint_auth_method'] = token_endpoint_auth_method
    if "apiBaseUrl" in value:
        api_base_url = value["apiBaseUrl"]
        if not (isinstance(api_base_url, str) and bool(api_base_url)):
            raise ValueError("OAuth 2.0 HTTP configuration apiBaseUrl has an invalid type")
        config_arguments['api_base_url'] = api_base_url
    else:
        raise ValueError("OAuth 2.0 HTTP configuration apiBaseUrl is required")
    if "maxBodyBytes" in value and value["maxBodyBytes"] != '':
        max_body_bytes = value["maxBodyBytes"]
        if not (type(max_body_bytes) is int):
            raise ValueError("OAuth 2.0 HTTP configuration maxBodyBytes has an invalid type")
        config_arguments['max_body_bytes'] = max_body_bytes
    if "requestTimeout" in value and value["requestTimeout"] != '':
        request_timeout = value["requestTimeout"]
        if not (type(request_timeout) is int):
            raise ValueError("OAuth 2.0 HTTP configuration requestTimeout has an invalid type")
        config_arguments['request_timeout_seconds'] = request_timeout / 1_000_000_000
    return Config(**config_arguments)
