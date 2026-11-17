# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_USER_DETAIL_DEFINITION
from ..models import GetUserDetailInput, UserDetail

class Connection:

    __slots__ = ('_get_user_detail', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_user_detail: Query[GetUserDetailInput, UserDetail],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_user_detail is None:
            raise ValueError("Easemob IM connection provider and operations are required")
        if get_user_detail.definition().operation != GET_USER_DETAIL_DEFINITION.operation:
            raise ValueError("Easemob IM connection operation identity differs")
        self.reference = reference
        self._get_user_detail = get_user_detail
        self._client = client

    def get_user_detail(self) -> Query[GetUserDetailInput, UserDetail]:
        return self._get_user_detail

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "easemob.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Easemob IM connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.easemob.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Easemob IM client is required")
    return Connection(reference, client.get_user_detail(), client)

setattr(new_connection, '__module__', 'dex_connectors.easemob.connection')


def new_connection_with_operations(
    get_user_detail: Query[GetUserDetailInput, UserDetail],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_user_detail)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.easemob.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Easemob IM stored credential does not match the connector credential fields")
        if set(material) - set(('appKey', 'clientId', 'clientSecret')):
            raise ValueError("Easemob IM stored credential does not match the connector credential fields")
        app_key = material.get("appKey")
        if app_key is not None and not (isinstance(app_key, str)):
            raise ValueError("Easemob IM credential appKey has an invalid type")
        client_id = material.get("clientId")
        if client_id is not None and not (isinstance(client_id, str)):
            raise ValueError("Easemob IM credential clientId has an invalid type")
        client_secret = material.get("clientSecret")
        if client_secret is not None and not (isinstance(client_secret, str)):
            raise ValueError("Easemob IM credential clientSecret has an invalid type")
        if not app_key:
            raise ValueError("Easemob IM credential appKey is required")
        if not client_id:
            raise ValueError("Easemob IM credential clientId is required")
        if not client_secret:
            raise ValueError("Easemob IM credential clientSecret is required")
        return Credentials(app_key, client_id, SecretString(client_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Easemob IM connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Easemob IM project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Easemob IM project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.easemob.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Easemob IM configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Easemob IM configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    else:
        raise ValueError("Easemob IM configuration baseUrl is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Easemob IM configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Easemob IM configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
