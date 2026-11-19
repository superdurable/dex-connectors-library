# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_USERS_DEFINITION
from ..models import ListUsersInput, ListUsersOutput

class Connection:

    __slots__ = ('_list_users', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_users: Query[ListUsersInput, ListUsersOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_users is None:
            raise ValueError("Kingsoft Cloud connection provider and operations are required")
        if list_users.definition().operation != LIST_USERS_DEFINITION.operation:
            raise ValueError("Kingsoft Cloud connection operation identity differs")
        self.reference = reference
        self._list_users = list_users
        self._client = client

    def list_users(self) -> Query[ListUsersInput, ListUsersOutput]:
        return self._list_users

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "kingsoftcloud.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Kingsoft Cloud connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.kingsoftcloud.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Kingsoft Cloud client is required")
    return Connection(reference, client.list_users(), client)

setattr(new_connection, '__module__', 'dex_connectors.kingsoftcloud.connection')


def new_connection_with_operations(
    list_users: Query[ListUsersInput, ListUsersOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_users)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.kingsoftcloud.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Kingsoft Cloud stored credential does not match the connector credential fields")
        if set(material) - set(('access_key_id', 'secret_access_key')):
            raise ValueError("Kingsoft Cloud stored credential does not match the connector credential fields")
        access_key_id = material.get("access_key_id")
        if access_key_id is not None and not (isinstance(access_key_id, str)):
            raise ValueError("Kingsoft Cloud credential access_key_id has an invalid type")
        secret_access_key = material.get("secret_access_key")
        if secret_access_key is not None and not (isinstance(secret_access_key, str)):
            raise ValueError("Kingsoft Cloud credential secret_access_key has an invalid type")
        if not access_key_id:
            raise ValueError("Kingsoft Cloud credential access_key_id is required")
        if not secret_access_key:
            raise ValueError("Kingsoft Cloud credential secret_access_key is required")
        return Credentials(SecretString(access_key_id), SecretString(secret_access_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Kingsoft Cloud connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Kingsoft Cloud project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Kingsoft Cloud project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.kingsoftcloud.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes", "region", "service"}:
        raise ValueError("Kingsoft Cloud configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Kingsoft Cloud configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Kingsoft Cloud configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Kingsoft Cloud configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "region" in value:
        region = value["region"]
        if not (isinstance(region, str)):
            raise ValueError("Kingsoft Cloud configuration region has an invalid type")
        config_arguments['region'] = region
    if "service" in value:
        service = value["service"]
        if not (isinstance(service, str)):
            raise ValueError("Kingsoft Cloud configuration service has an invalid type")
        config_arguments['service'] = service
    return Config(**config_arguments)
