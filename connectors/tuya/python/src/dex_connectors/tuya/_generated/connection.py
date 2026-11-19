# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_DEVICE_DETAILS_DEFINITION
from ..models import GetDeviceDetailsInput, GetDeviceDetailsOutput

class Connection:

    __slots__ = ('_get_device_details', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_device_details: Query[GetDeviceDetailsInput, GetDeviceDetailsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_device_details is None:
            raise ValueError("Tuya connection provider and operations are required")
        if get_device_details.definition().operation != GET_DEVICE_DETAILS_DEFINITION.operation:
            raise ValueError("Tuya connection operation identity differs")
        self.reference = reference
        self._get_device_details = get_device_details
        self._client = client

    def get_device_details(self) -> Query[GetDeviceDetailsInput, GetDeviceDetailsOutput]:
        return self._get_device_details

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "tuya.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Tuya connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.tuya.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Tuya client is required")
    return Connection(reference, client.get_device_details(), client)

setattr(new_connection, '__module__', 'dex_connectors.tuya.connection')


def new_connection_with_operations(
    get_device_details: Query[GetDeviceDetailsInput, GetDeviceDetailsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_device_details)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.tuya.connection')


class CredentialMaterialError(ValueError):
    "Stored Tuya credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("Tuya stored credential does not match the connector credential fields")
        if set(material) - set(('client_id', 'client_secret')):
            raise CredentialMaterialError("Tuya stored credential does not match the connector credential fields")
        client_id = material.get("client_id")
        if client_id is not None and not (isinstance(client_id, str)):
            raise CredentialMaterialError("Tuya credential client_id has an invalid type")
        client_secret = material.get("client_secret")
        if client_secret is not None and not (isinstance(client_secret, str)):
            raise CredentialMaterialError("Tuya credential client_secret has an invalid type")
        if not client_id:
            raise CredentialMaterialError("Tuya credential client_id is required")
        if not client_secret:
            raise CredentialMaterialError("Tuya credential client_secret is required")
        return Credentials(SecretString(client_id), SecretString(client_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Tuya connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Tuya project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Tuya project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.tuya.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "timeout", "maxResponseBytes"}:
        raise ValueError("Tuya configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str) and endpoint in ('https://openapi.tuyacn.com', 'https://openapi.tuyaus.com', 'https://openapi-ueaz.tuyaus.com', 'https://openapi.tuyaeu.com', 'https://openapi-weaz.tuyaeu.com', 'https://openapi.tuyain.com', 'https://openapi-sg.iotbing.com')):
            raise ValueError("Tuya configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Tuya configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Tuya configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
