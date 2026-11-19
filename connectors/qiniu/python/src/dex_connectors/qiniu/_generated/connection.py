# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_BUCKETS_DEFINITION
from ..models import ListBucketsInput, ListBucketsOutput

class Connection:

    __slots__ = ('_list_buckets', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_buckets: Query[ListBucketsInput, ListBucketsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_buckets is None:
            raise ValueError("Qiniu Kodo connection provider and operations are required")
        if list_buckets.definition().operation != LIST_BUCKETS_DEFINITION.operation:
            raise ValueError("Qiniu Kodo connection operation identity differs")
        self.reference = reference
        self._list_buckets = list_buckets
        self._client = client

    def list_buckets(self) -> Query[ListBucketsInput, ListBucketsOutput]:
        return self._list_buckets

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "qiniu.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Qiniu Kodo connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.qiniu.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Qiniu Kodo client is required")
    return Connection(reference, client.list_buckets(), client)

setattr(new_connection, '__module__', 'dex_connectors.qiniu.connection')


def new_connection_with_operations(
    list_buckets: Query[ListBucketsInput, ListBucketsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_buckets)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.qiniu.connection')


class CredentialMaterialError(ValueError):
    "Stored Qiniu Kodo credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("Qiniu Kodo stored credential does not match the connector credential fields")
        if set(material) - set(('access_key', 'secret_key')):
            raise CredentialMaterialError("Qiniu Kodo stored credential does not match the connector credential fields")
        access_key = material.get("access_key")
        if access_key is not None and not (isinstance(access_key, str)):
            raise CredentialMaterialError("Qiniu Kodo credential access_key has an invalid type")
        secret_key = material.get("secret_key")
        if secret_key is not None and not (isinstance(secret_key, str)):
            raise CredentialMaterialError("Qiniu Kodo credential secret_key has an invalid type")
        if not access_key:
            raise CredentialMaterialError("Qiniu Kodo credential access_key is required")
        if not secret_key:
            raise CredentialMaterialError("Qiniu Kodo credential secret_key is required")
        return Credentials(SecretString(access_key), SecretString(secret_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Qiniu Kodo connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Qiniu Kodo project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Qiniu Kodo project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.qiniu.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Qiniu Kodo configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Qiniu Kodo configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Qiniu Kodo configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Qiniu Kodo configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
