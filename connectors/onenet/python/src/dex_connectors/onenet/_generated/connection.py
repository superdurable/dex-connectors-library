# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, QUERY_DEVICE_DETAIL_DEFINITION
from ..models import DeviceDetail, QueryDeviceDetailInput

class Connection:

    __slots__ = ('_query_device_detail', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        query_device_detail: Query[QueryDeviceDetailInput, DeviceDetail],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query_device_detail is None:
            raise ValueError("OneNET AIoT connection provider and operations are required")
        if query_device_detail.definition().operation != QUERY_DEVICE_DETAIL_DEFINITION.operation:
            raise ValueError("OneNET AIoT connection operation identity differs")
        self.reference = reference
        self._query_device_detail = query_device_detail
        self._client = client

    def query_device_detail(self) -> Query[QueryDeviceDetailInput, DeviceDetail]:
        return self._query_device_detail

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "onenet.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("OneNET AIoT connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.onenet.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("OneNET AIoT client is required")
    return Connection(reference, client.query_device_detail(), client)

setattr(new_connection, '__module__', 'dex_connectors.onenet.connection')


def new_connection_with_operations(
    query_device_detail: Query[QueryDeviceDetailInput, DeviceDetail],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, query_device_detail)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.onenet.connection')


class CredentialMaterialError(ValueError):
    "Stored OneNET AIoT credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("OneNET AIoT stored credential does not match the connector credential fields")
        if set(material) - set(('userId', 'accessKey', 'productId')):
            raise CredentialMaterialError("OneNET AIoT stored credential does not match the connector credential fields")
        user_id = material.get("userId")
        if user_id is not None and not (isinstance(user_id, str)):
            raise CredentialMaterialError("OneNET AIoT credential userId has an invalid type")
        access_key = material.get("accessKey")
        if access_key is not None and not (isinstance(access_key, str)):
            raise CredentialMaterialError("OneNET AIoT credential accessKey has an invalid type")
        product_id = material.get("productId")
        if product_id is not None and not (isinstance(product_id, str)):
            raise CredentialMaterialError("OneNET AIoT credential productId has an invalid type")
        if not user_id:
            raise CredentialMaterialError("OneNET AIoT credential userId is required")
        if not access_key:
            raise CredentialMaterialError("OneNET AIoT credential accessKey is required")
        if not product_id:
            raise CredentialMaterialError("OneNET AIoT credential productId is required")
        return Credentials(user_id, SecretString(access_key), product_id)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("OneNET AIoT connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("OneNET AIoT project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("OneNET AIoT project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.onenet.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("OneNET AIoT configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("OneNET AIoT configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("OneNET AIoT configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("OneNET AIoT configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
