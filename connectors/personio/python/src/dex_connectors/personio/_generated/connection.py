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
from .definitions import CONNECTOR_ID, GET_PERSON_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import GetPersonInput, PersonProfile

class Connection:

    __slots__ = ('_get_person', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_person: Query[GetPersonInput, PersonProfile],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_person is None:
            raise ValueError("Personio connection provider and operations are required")
        if get_person.definition().operation != GET_PERSON_DEFINITION.operation:
            raise ValueError("Personio connection operation identity differs")
        self.reference = reference
        self._get_person = get_person
        self._client = client

    def get_person(self) -> Query[GetPersonInput, PersonProfile]:
        return self._get_person

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "personio.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Personio connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.personio.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Personio client is required")
    return Connection(reference, client.get_person(), client)

setattr(new_connection, '__module__', 'dex_connectors.personio.connection')


def new_connection_with_operations(
    get_person: Query[GetPersonInput, PersonProfile],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_person)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.personio.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Personio stored credential does not match the connector credential fields")
    if set(material) - set(('client_id', 'client_secret', 'access_token')):
        raise ValueError("Personio stored credential does not match the connector credential fields")
    is_shared = shared
    client_id = material.get("client_id")
    if client_id is not None and not (isinstance(client_id, str)):
        raise ValueError("Personio credential client_id has an invalid type")
    client_secret = material.get("client_secret")
    if client_secret is not None and not (isinstance(client_secret, str)):
        raise ValueError("Personio credential client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Personio credential access_token has an invalid type")
    if not client_id:
        raise ValueError("Personio credential client_id is required")
    if not client_secret and not is_shared:
        raise ValueError("Personio credential client_secret is required")
    if not access_token and is_shared:
        raise ValueError("Personio credential access_token is required")
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
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Personio", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Personio connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Personio project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Personio project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.personio.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Personio configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value and value["baseUrl"] != '':
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Personio configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value and value["timeout"] != '':
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Personio configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Personio configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
