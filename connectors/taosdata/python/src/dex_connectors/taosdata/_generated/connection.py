# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_DATABASES_DEFINITION
from ..models import ListDatabasesInput, ListDatabasesOutput

class Connection:

    __slots__ = ('_list_databases', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_databases: Query[ListDatabasesInput, ListDatabasesOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_databases is None:
            raise ValueError("TDengine Cloud connection provider and operations are required")
        if list_databases.definition().operation != LIST_DATABASES_DEFINITION.operation:
            raise ValueError("TDengine Cloud connection operation identity differs")
        self.reference = reference
        self._list_databases = list_databases
        self._client = client

    def list_databases(self) -> Query[ListDatabasesInput, ListDatabasesOutput]:
        return self._list_databases

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "taosdata.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("TDengine Cloud connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.taosdata.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("TDengine Cloud client is required")
    return Connection(reference, client.list_databases(), client)

setattr(new_connection, '__module__', 'dex_connectors.taosdata.connection')


def new_connection_with_operations(
    list_databases: Query[ListDatabasesInput, ListDatabasesOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_databases)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.taosdata.connection')


class CredentialMaterialError(ValueError):
    "Stored TDengine Cloud credential material fails the connector credential checks, so no retry can succeed."


def is_credential_material_error(error: BaseException) -> bool:
    """Report malformed stored credential material, also behind the SDK provider's decoding ValueError."""
    if isinstance(error, CredentialMaterialError):
        return True
    # Only the SDK decoding wrapper qualifies; other errors may be transient.
    return (
        type(error) is ValueError
        and str(error) in {'project credential decoding failed', 'shared project credential decoding failed'}
        and isinstance(error.__context__, CredentialMaterialError)
    )


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise CredentialMaterialError("TDengine Cloud stored credential does not match the connector credential fields")
        if set(material) - set(('cloud_url', 'cloud_token')):
            raise CredentialMaterialError("TDengine Cloud stored credential does not match the connector credential fields")
        cloud_url = material.get("cloud_url")
        if cloud_url is not None and not (isinstance(cloud_url, str)):
            raise CredentialMaterialError("TDengine Cloud credential cloud_url has an invalid type")
        cloud_token = material.get("cloud_token")
        if cloud_token is not None and not (isinstance(cloud_token, str)):
            raise CredentialMaterialError("TDengine Cloud credential cloud_token has an invalid type")
        if not cloud_url:
            raise CredentialMaterialError("TDengine Cloud credential cloud_url is required")
        if not cloud_token:
            raise CredentialMaterialError("TDengine Cloud credential cloud_token is required")
        return Credentials(SecretString(cloud_url), SecretString(cloud_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("TDengine Cloud connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("TDengine Cloud project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("TDengine Cloud project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.taosdata.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("TDengine Cloud configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("TDengine Cloud configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("TDengine Cloud configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
