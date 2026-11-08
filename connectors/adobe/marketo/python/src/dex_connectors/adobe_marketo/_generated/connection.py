# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString, CredentialRefreshDriver
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, PROVIDER, GET_LEAD_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import GetLeadInput, LeadProfile

class Connection:

    __slots__ = ('_get_lead', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_lead: Query[GetLeadInput, LeadProfile],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or get_lead is None:
            raise ValueError("Adobe Marketo Engage connection provider and operations are required")
        if get_lead.definition().operation != GET_LEAD_DEFINITION.operation:
            raise ValueError("Adobe Marketo Engage connection operation identity differs")
        self.reference = reference
        self._get_lead = get_lead
        self._client = client

    def get_lead(self) -> Query[GetLeadInput, LeadProfile]:
        return self._get_lead

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "adobe_marketo.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Adobe Marketo Engage connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.adobe_marketo.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Adobe Marketo Engage client is required")
    return Connection(reference, client.get_lead(), client)

setattr(new_connection, '__module__', 'dex_connectors.adobe_marketo.connection')


def new_connection_with_operations(
    get_lead: Query[GetLeadInput, LeadProfile],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_lead)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.adobe_marketo.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Adobe Marketo Engage stored credential does not match the connector credential fields")
    if set(material) - set(('client_id', 'client_secret', 'access_token')):
        raise ValueError("Adobe Marketo Engage stored credential does not match the connector credential fields")
    is_shared = shared
    client_id = material.get("client_id")
    if client_id is not None and not (isinstance(client_id, str)):
        raise ValueError("Adobe Marketo Engage credential client_id has an invalid type")
    client_secret = material.get("client_secret")
    if client_secret is not None and not (isinstance(client_secret, str)):
        raise ValueError("Adobe Marketo Engage credential client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Adobe Marketo Engage credential access_token has an invalid type")
    if not client_id:
        raise ValueError("Adobe Marketo Engage credential client_id is required")
    if not client_secret and not is_shared:
        raise ValueError("Adobe Marketo Engage credential client_secret is required")
    if not access_token and is_shared:
        raise ValueError("Adobe Marketo Engage credential access_token is required")
    return Credentials(client_id, SecretString(client_secret or ''), SecretString(access_token or ''))


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    if credentials.client_id:
        material["client_id"] = credentials.client_id
    if credentials.client_secret.reveal():
        material["client_secret"] = credentials.client_secret.reveal()
    if credentials.access_token.reveal():
        material["access_token"] = credentials.access_token.reveal()
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Adobe Marketo Engage", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Adobe Marketo Engage connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Adobe Marketo Engage project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Adobe Marketo Engage project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.adobe_marketo.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"restEndpoint", "tokenEndpoint", "timeout", "maxResponseBytes"}:
        raise ValueError("Adobe Marketo Engage configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "restEndpoint" in value:
        rest_endpoint = value["restEndpoint"]
        if not (isinstance(rest_endpoint, str) and bool(rest_endpoint)):
            raise ValueError("Adobe Marketo Engage configuration restEndpoint has an invalid type")
        config_arguments['rest_endpoint'] = rest_endpoint
    else:
        raise ValueError("Adobe Marketo Engage configuration restEndpoint is required")
    if "tokenEndpoint" in value:
        token_endpoint = value["tokenEndpoint"]
        if not (isinstance(token_endpoint, str) and bool(token_endpoint)):
            raise ValueError("Adobe Marketo Engage configuration tokenEndpoint has an invalid type")
        config_arguments['token_endpoint'] = token_endpoint
    else:
        raise ValueError("Adobe Marketo Engage configuration tokenEndpoint is required")
    if "timeout" in value and value["timeout"] != '':
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Adobe Marketo Engage configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Adobe Marketo Engage configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
