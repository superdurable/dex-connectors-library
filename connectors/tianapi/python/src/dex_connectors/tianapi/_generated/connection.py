# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_HOLIDAY_STATUS_DEFINITION
from ..models import GetHolidayStatusInput, GetHolidayStatusOutput

class Connection:

    __slots__ = ('_get_holiday_status', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_holiday_status: Query[GetHolidayStatusInput, GetHolidayStatusOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_holiday_status is None:
            raise ValueError("TianAPI connection provider and operations are required")
        if get_holiday_status.definition().operation != GET_HOLIDAY_STATUS_DEFINITION.operation:
            raise ValueError("TianAPI connection operation identity differs")
        self.reference = reference
        self._get_holiday_status = get_holiday_status
        self._client = client

    def get_holiday_status(self) -> Query[GetHolidayStatusInput, GetHolidayStatusOutput]:
        return self._get_holiday_status

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "tianapi.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("TianAPI connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.tianapi.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("TianAPI client is required")
    return Connection(reference, client.get_holiday_status(), client)

setattr(new_connection, '__module__', 'dex_connectors.tianapi.connection')


def new_connection_with_operations(
    get_holiday_status: Query[GetHolidayStatusInput, GetHolidayStatusOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_holiday_status)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.tianapi.connection')


class CredentialMaterialError(ValueError):
    "Stored TianAPI credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("TianAPI stored credential does not match the connector credential fields")
        if set(material) - set(('key',)):
            raise CredentialMaterialError("TianAPI stored credential does not match the connector credential fields")
        key = material.get("key")
        if key is not None and not (isinstance(key, str)):
            raise CredentialMaterialError("TianAPI credential key has an invalid type")
        if not key:
            raise CredentialMaterialError("TianAPI credential key is required")
        return Credentials(SecretString(key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("TianAPI connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("TianAPI project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("TianAPI project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.tianapi.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("TianAPI configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("TianAPI configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("TianAPI configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
