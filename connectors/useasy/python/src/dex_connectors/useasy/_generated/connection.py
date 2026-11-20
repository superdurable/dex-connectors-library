# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_AGENTS_DEFINITION
from ..models import ListAgentsInput, ListAgentsOutput

class Connection:

    __slots__ = ('_list_agents', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_agents: Query[ListAgentsInput, ListAgentsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_agents is None:
            raise ValueError("Useasy connection provider and operations are required")
        if list_agents.definition().operation != LIST_AGENTS_DEFINITION.operation:
            raise ValueError("Useasy connection operation identity differs")
        self.reference = reference
        self._list_agents = list_agents
        self._client = client

    def list_agents(self) -> Query[ListAgentsInput, ListAgentsOutput]:
        return self._list_agents

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "useasy.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Useasy connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.useasy.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Useasy client is required")
    return Connection(reference, client.list_agents(), client)

setattr(new_connection, '__module__', 'dex_connectors.useasy.connection')


def new_connection_with_operations(
    list_agents: Query[ListAgentsInput, ListAgentsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_agents)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.useasy.connection')


class CredentialMaterialError(ValueError):
    "Stored Useasy credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("Useasy stored credential does not match the connector credential fields")
        if set(material) - set(('app_id', 'secret_key')):
            raise CredentialMaterialError("Useasy stored credential does not match the connector credential fields")
        app_id = material.get("app_id")
        if app_id is not None and not (isinstance(app_id, str)):
            raise CredentialMaterialError("Useasy credential app_id has an invalid type")
        secret_key = material.get("secret_key")
        if secret_key is not None and not (isinstance(secret_key, str)):
            raise CredentialMaterialError("Useasy credential secret_key has an invalid type")
        if not app_id:
            raise CredentialMaterialError("Useasy credential app_id is required")
        if not secret_key:
            raise CredentialMaterialError("Useasy credential secret_key is required")
        return Credentials(SecretString(app_id), SecretString(secret_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Useasy connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Useasy project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Useasy project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.useasy.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Useasy configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Useasy configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Useasy configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Useasy configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
