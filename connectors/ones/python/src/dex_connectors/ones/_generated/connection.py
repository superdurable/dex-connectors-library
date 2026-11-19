# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_TEAMS_DEFINITION
from ..models import ListTeamsInput, ListTeamsOutput

class Connection:

    __slots__ = ('_list_teams', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_teams: Query[ListTeamsInput, ListTeamsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_teams is None:
            raise ValueError("ONES connection provider and operations are required")
        if list_teams.definition().operation != LIST_TEAMS_DEFINITION.operation:
            raise ValueError("ONES connection operation identity differs")
        self.reference = reference
        self._list_teams = list_teams
        self._client = client

    def list_teams(self) -> Query[ListTeamsInput, ListTeamsOutput]:
        return self._list_teams

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "ones.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("ONES connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.ones.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("ONES client is required")
    return Connection(reference, client.list_teams(), client)

setattr(new_connection, '__module__', 'dex_connectors.ones.connection')


def new_connection_with_operations(
    list_teams: Query[ListTeamsInput, ListTeamsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_teams)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.ones.connection')


class CredentialMaterialError(ValueError):
    "Stored ONES credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("ONES stored credential does not match the connector credential fields")
        if set(material) - set(('api_key',)):
            raise CredentialMaterialError("ONES stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise CredentialMaterialError("ONES credential api_key has an invalid type")
        if not api_key:
            raise CredentialMaterialError("ONES credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("ONES connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("ONES project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("ONES project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.ones.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("ONES configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("ONES configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    else:
        raise ValueError("ONES configuration baseUrl is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("ONES configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("ONES configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
