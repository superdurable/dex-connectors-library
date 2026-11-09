# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_BLOB_DEFINITION
from ..models import BlobDownload, GetBlobInput

class Connection:

    __slots__ = ('_get_blob', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_blob: Query[GetBlobInput, BlobDownload],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_blob is None:
            raise ValueError("Azure Blob Storage connection provider and operations are required")
        if get_blob.definition().operation != GET_BLOB_DEFINITION.operation:
            raise ValueError("Azure Blob Storage connection operation identity differs")
        self.reference = reference
        self._get_blob = get_blob
        self._client = client

    def get_blob(self) -> Query[GetBlobInput, BlobDownload]:
        return self._get_blob

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "microsoft_azure_blob.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Azure Blob Storage connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.microsoft_azure_blob.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Azure Blob Storage client is required")
    return Connection(reference, client.get_blob(), client)

setattr(new_connection, '__module__', 'dex_connectors.microsoft_azure_blob.connection')


def new_connection_with_operations(
    get_blob: Query[GetBlobInput, BlobDownload],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_blob)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.microsoft_azure_blob.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Azure Blob Storage stored credential does not match the connector credential fields")
        if set(material) - set(('account_key',)):
            raise ValueError("Azure Blob Storage stored credential does not match the connector credential fields")
        account_key = material.get("account_key")
        if account_key is not None and not (isinstance(account_key, str)):
            raise ValueError("Azure Blob Storage credential account_key has an invalid type")
        if not account_key:
            raise ValueError("Azure Blob Storage credential account_key is required")
        return Credentials(SecretString(account_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Azure Blob Storage connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Azure Blob Storage project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Azure Blob Storage project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.microsoft_azure_blob.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"accountName", "endpoint", "timeout", "maxResponseBytes"}:
        raise ValueError("Azure Blob Storage configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "accountName" in value:
        account_name = value["accountName"]
        if not (isinstance(account_name, str)):
            raise ValueError("Azure Blob Storage configuration accountName has an invalid type")
        config_arguments['account_name'] = account_name
    else:
        raise ValueError("Azure Blob Storage configuration accountName is required")
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Azure Blob Storage configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    else:
        config_arguments['endpoint'] = ''
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Azure Blob Storage configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Azure Blob Storage configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
