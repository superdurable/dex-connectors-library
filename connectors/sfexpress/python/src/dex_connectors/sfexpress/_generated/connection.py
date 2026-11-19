# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, QUERY_ROUTES_DEFINITION
from ..models import QueryRoutesInput, RouteQueryResult

class Connection:

    __slots__ = ('_query_routes', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        query_routes: Query[QueryRoutesInput, RouteQueryResult],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query_routes is None:
            raise ValueError("SF Express connection provider and operations are required")
        if query_routes.definition().operation != QUERY_ROUTES_DEFINITION.operation:
            raise ValueError("SF Express connection operation identity differs")
        self.reference = reference
        self._query_routes = query_routes
        self._client = client

    def query_routes(self) -> Query[QueryRoutesInput, RouteQueryResult]:
        return self._query_routes

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "sfexpress.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("SF Express connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.sfexpress.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("SF Express client is required")
    return Connection(reference, client.query_routes(), client)

setattr(new_connection, '__module__', 'dex_connectors.sfexpress.connection')


def new_connection_with_operations(
    query_routes: Query[QueryRoutesInput, RouteQueryResult],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, query_routes)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.sfexpress.connection')


class CredentialMaterialError(ValueError):
    "Stored SF Express credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("SF Express stored credential does not match the connector credential fields")
        if set(material) - set(('partner_id', 'checkword')):
            raise CredentialMaterialError("SF Express stored credential does not match the connector credential fields")
        partner_id = material.get("partner_id")
        if partner_id is not None and not (isinstance(partner_id, str)):
            raise CredentialMaterialError("SF Express credential partner_id has an invalid type")
        checkword = material.get("checkword")
        if checkword is not None and not (isinstance(checkword, str)):
            raise CredentialMaterialError("SF Express credential checkword has an invalid type")
        if not partner_id:
            raise CredentialMaterialError("SF Express credential partner_id is required")
        if not checkword:
            raise CredentialMaterialError("SF Express credential checkword is required")
        return Credentials(partner_id, SecretString(checkword))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("SF Express connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("SF Express project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("SF Express project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.sfexpress.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("SF Express configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("SF Express configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("SF Express configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("SF Express configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
