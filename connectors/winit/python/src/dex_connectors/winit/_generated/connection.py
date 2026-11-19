# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_WAREHOUSES_DEFINITION
from ..models import ListWarehousesInput, WarehousePage

class Connection:

    __slots__ = ('_list_warehouses', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_warehouses: Query[ListWarehousesInput, WarehousePage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_warehouses is None:
            raise ValueError("Winit connection provider and operations are required")
        if list_warehouses.definition().operation != LIST_WAREHOUSES_DEFINITION.operation:
            raise ValueError("Winit connection operation identity differs")
        self.reference = reference
        self._list_warehouses = list_warehouses
        self._client = client

    def list_warehouses(self) -> Query[ListWarehousesInput, WarehousePage]:
        return self._list_warehouses

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "winit.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Winit connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.winit.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Winit client is required")
    return Connection(reference, client.list_warehouses(), client)

setattr(new_connection, '__module__', 'dex_connectors.winit.connection')


def new_connection_with_operations(
    list_warehouses: Query[ListWarehousesInput, WarehousePage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_warehouses)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.winit.connection')


class CredentialMaterialError(ValueError):
    "Stored Winit credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("Winit stored credential does not match the connector credential fields")
        if set(material) - set(('app_key', 'token', 'client_id', 'client_secret')):
            raise CredentialMaterialError("Winit stored credential does not match the connector credential fields")
        app_key = material.get("app_key")
        if app_key is not None and not (isinstance(app_key, str)):
            raise CredentialMaterialError("Winit credential app_key has an invalid type")
        token = material.get("token")
        if token is not None and not (isinstance(token, str)):
            raise CredentialMaterialError("Winit credential token has an invalid type")
        client_id = material.get("client_id")
        if client_id is not None and not (isinstance(client_id, str)):
            raise CredentialMaterialError("Winit credential client_id has an invalid type")
        client_secret = material.get("client_secret")
        if client_secret is not None and not (isinstance(client_secret, str)):
            raise CredentialMaterialError("Winit credential client_secret has an invalid type")
        if not app_key:
            raise CredentialMaterialError("Winit credential app_key is required")
        if not token:
            raise CredentialMaterialError("Winit credential token is required")
        if not client_id:
            raise CredentialMaterialError("Winit credential client_id is required")
        if not client_secret:
            raise CredentialMaterialError("Winit credential client_secret is required")
        return Credentials(SecretString(app_key), SecretString(token), SecretString(client_id), SecretString(client_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Winit connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Winit project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Winit project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.winit.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "platform", "timeout", "maxResponseBytes"}:
        raise ValueError("Winit configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Winit configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "platform" in value:
        platform = value["platform"]
        if not (isinstance(platform, str)):
            raise ValueError("Winit configuration platform has an invalid type")
        config_arguments['platform'] = platform
    else:
        raise ValueError("Winit configuration platform is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Winit configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Winit configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
