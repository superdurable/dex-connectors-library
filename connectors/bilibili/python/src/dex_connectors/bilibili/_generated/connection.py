# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_AUTHORIZED_USER_DEFINITION
from ..models import AuthorizedUser, GetAuthorizedUserInput

class Connection:

    __slots__ = ('_get_authorized_user', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_authorized_user: Query[GetAuthorizedUserInput, AuthorizedUser],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_authorized_user is None:
            raise ValueError("Bilibili connection provider and operations are required")
        if get_authorized_user.definition().operation != GET_AUTHORIZED_USER_DEFINITION.operation:
            raise ValueError("Bilibili connection operation identity differs")
        self.reference = reference
        self._get_authorized_user = get_authorized_user
        self._client = client

    def get_authorized_user(self) -> Query[GetAuthorizedUserInput, AuthorizedUser]:
        return self._get_authorized_user

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "bilibili.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Bilibili connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.bilibili.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Bilibili client is required")
    return Connection(reference, client.get_authorized_user(), client)

setattr(new_connection, '__module__', 'dex_connectors.bilibili.connection')


def new_connection_with_operations(
    get_authorized_user: Query[GetAuthorizedUserInput, AuthorizedUser],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_authorized_user)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.bilibili.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Bilibili stored credential does not match the connector credential fields")
        if set(material) - set(('clientID', 'appSecret', 'accessToken')):
            raise ValueError("Bilibili stored credential does not match the connector credential fields")
        client_id = material.get("clientID")
        if client_id is not None and not (isinstance(client_id, str)):
            raise ValueError("Bilibili credential clientID has an invalid type")
        app_secret = material.get("appSecret")
        if app_secret is not None and not (isinstance(app_secret, str)):
            raise ValueError("Bilibili credential appSecret has an invalid type")
        access_token = material.get("accessToken")
        if access_token is not None and not (isinstance(access_token, str)):
            raise ValueError("Bilibili credential accessToken has an invalid type")
        if not client_id:
            raise ValueError("Bilibili credential clientID is required")
        if not app_secret:
            raise ValueError("Bilibili credential appSecret is required")
        if not access_token:
            raise ValueError("Bilibili credential accessToken is required")
        return Credentials(client_id, SecretString(app_secret), SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Bilibili connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Bilibili project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Bilibili project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.bilibili.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Bilibili configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Bilibili configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Bilibili configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Bilibili configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
