# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_REPOSITORY_DEFINITION
from ..models import GetRepositoryInput, GetRepositoryOutput

class Connection:

    __slots__ = ('_get_repository', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_repository: Query[GetRepositoryInput, GetRepositoryOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_repository is None:
            raise ValueError("GitCode connection provider and operations are required")
        if get_repository.definition().operation != GET_REPOSITORY_DEFINITION.operation:
            raise ValueError("GitCode connection operation identity differs")
        self.reference = reference
        self._get_repository = get_repository
        self._client = client

    def get_repository(self) -> Query[GetRepositoryInput, GetRepositoryOutput]:
        return self._get_repository

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "gitcode.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("GitCode connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.gitcode.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("GitCode client is required")
    return Connection(reference, client.get_repository(), client)

setattr(new_connection, '__module__', 'dex_connectors.gitcode.connection')


def new_connection_with_operations(
    get_repository: Query[GetRepositoryInput, GetRepositoryOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_repository)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.gitcode.connection')


class CredentialMaterialError(ValueError):
    "Stored GitCode credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("GitCode stored credential does not match the connector credential fields")
        if set(material) - set(('access_token',)):
            raise CredentialMaterialError("GitCode stored credential does not match the connector credential fields")
        access_token = material.get("access_token")
        if access_token is not None and not (isinstance(access_token, str)):
            raise CredentialMaterialError("GitCode credential access_token has an invalid type")
        if not access_token:
            raise CredentialMaterialError("GitCode credential access_token is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("GitCode connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("GitCode project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("GitCode project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.gitcode.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("GitCode configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("GitCode configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("GitCode configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("GitCode configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
