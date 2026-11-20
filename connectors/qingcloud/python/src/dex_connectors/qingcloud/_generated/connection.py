# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, DESCRIBE_ZONES_DEFINITION
from ..models import DescribeZonesInput, ZonesPage

class Connection:

    __slots__ = ('_describe_zones', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        describe_zones: Query[DescribeZonesInput, ZonesPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or describe_zones is None:
            raise ValueError("QingCloud connection provider and operations are required")
        if describe_zones.definition().operation != DESCRIBE_ZONES_DEFINITION.operation:
            raise ValueError("QingCloud connection operation identity differs")
        self.reference = reference
        self._describe_zones = describe_zones
        self._client = client

    def describe_zones(self) -> Query[DescribeZonesInput, ZonesPage]:
        return self._describe_zones

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "qingcloud.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("QingCloud connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.qingcloud.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("QingCloud client is required")
    return Connection(reference, client.describe_zones(), client)

setattr(new_connection, '__module__', 'dex_connectors.qingcloud.connection')


def new_connection_with_operations(
    describe_zones: Query[DescribeZonesInput, ZonesPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, describe_zones)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.qingcloud.connection')


class CredentialMaterialError(ValueError):
    "Stored QingCloud credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("QingCloud stored credential does not match the connector credential fields")
        if set(material) - set(('accessKeyId', 'secretAccessKey')):
            raise CredentialMaterialError("QingCloud stored credential does not match the connector credential fields")
        access_key_id = material.get("accessKeyId")
        if access_key_id is not None and not (isinstance(access_key_id, str)):
            raise CredentialMaterialError("QingCloud credential accessKeyId has an invalid type")
        secret_access_key = material.get("secretAccessKey")
        if secret_access_key is not None and not (isinstance(secret_access_key, str)):
            raise CredentialMaterialError("QingCloud credential secretAccessKey has an invalid type")
        if not access_key_id:
            raise CredentialMaterialError("QingCloud credential accessKeyId is required")
        if not secret_access_key:
            raise CredentialMaterialError("QingCloud credential secretAccessKey is required")
        return Credentials(access_key_id, SecretString(secret_access_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("QingCloud connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("QingCloud project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("QingCloud project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.qingcloud.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("QingCloud configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("QingCloud configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("QingCloud configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("QingCloud configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
