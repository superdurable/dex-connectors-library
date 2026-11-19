# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_BALANCE_DEFINITION
from ..models import Balance, GetBalanceInput

class Connection:

    __slots__ = ('_get_balance', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_balance: Query[GetBalanceInput, Balance],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_balance is None:
            raise ValueError("Shumei connection provider and operations are required")
        if get_balance.definition().operation != GET_BALANCE_DEFINITION.operation:
            raise ValueError("Shumei connection operation identity differs")
        self.reference = reference
        self._get_balance = get_balance
        self._client = client

    def get_balance(self) -> Query[GetBalanceInput, Balance]:
        return self._get_balance

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "shumei.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Shumei connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.shumei.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Shumei client is required")
    return Connection(reference, client.get_balance(), client)

setattr(new_connection, '__module__', 'dex_connectors.shumei.connection')


def new_connection_with_operations(
    get_balance: Query[GetBalanceInput, Balance],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_balance)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.shumei.connection')


class CredentialMaterialError(ValueError):
    "Stored Shumei credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("Shumei stored credential does not match the connector credential fields")
        if set(material) - set(('access_key',)):
            raise CredentialMaterialError("Shumei stored credential does not match the connector credential fields")
        access_key = material.get("access_key")
        if access_key is not None and not (isinstance(access_key, str)):
            raise CredentialMaterialError("Shumei credential access_key has an invalid type")
        if not access_key:
            raise CredentialMaterialError("Shumei credential access_key is required")
        return Credentials(SecretString(access_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Shumei connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Shumei project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Shumei project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.shumei.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Shumei configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Shumei configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Shumei configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Shumei configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
