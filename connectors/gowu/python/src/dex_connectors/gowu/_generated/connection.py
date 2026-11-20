# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_ACCESS_CLIENT_DEFINITION
from ..models import AccessClientResult, GetAccessClientInput

class Connection:

    __slots__ = ('_get_access_client', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_access_client: Query[GetAccessClientInput, AccessClientResult],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_access_client is None:
            raise ValueError("Gowu connection provider and operations are required")
        if get_access_client.definition().operation != GET_ACCESS_CLIENT_DEFINITION.operation:
            raise ValueError("Gowu connection operation identity differs")
        self.reference = reference
        self._get_access_client = get_access_client
        self._client = client

    def get_access_client(self) -> Query[GetAccessClientInput, AccessClientResult]:
        return self._get_access_client

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "gowu.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Gowu connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.gowu.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Gowu client is required")
    return Connection(reference, client.get_access_client(), client)

setattr(new_connection, '__module__', 'dex_connectors.gowu.connection')


def new_connection_with_operations(
    get_access_client: Query[GetAccessClientInput, AccessClientResult],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_access_client)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.gowu.connection')


class CredentialMaterialError(ValueError):
    "Stored Gowu credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("Gowu stored credential does not match the connector credential fields")
        if set(material) - set(('app_key', 'app_secret')):
            raise CredentialMaterialError("Gowu stored credential does not match the connector credential fields")
        app_key = material.get("app_key")
        if app_key is not None and not (isinstance(app_key, str)):
            raise CredentialMaterialError("Gowu credential app_key has an invalid type")
        app_secret = material.get("app_secret")
        if app_secret is not None and not (isinstance(app_secret, str)):
            raise CredentialMaterialError("Gowu credential app_secret has an invalid type")
        if not app_key:
            raise CredentialMaterialError("Gowu credential app_key is required")
        if not app_secret:
            raise CredentialMaterialError("Gowu credential app_secret is required")
        return Credentials(app_key, SecretString(app_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Gowu connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Gowu project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Gowu project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.gowu.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Gowu configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Gowu configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Gowu configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Gowu configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
