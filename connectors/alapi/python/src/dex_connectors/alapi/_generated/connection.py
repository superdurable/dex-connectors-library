# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LOOKUP_IP_LOCATION_DEFINITION
from ..models import LookupIPLocationInput, LookupIPLocationOutput

class Connection:

    __slots__ = ('_lookup_ip_location', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        lookup_ip_location: Query[LookupIPLocationInput, LookupIPLocationOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or lookup_ip_location is None:
            raise ValueError("ALAPI IP Lookup connection provider and operations are required")
        if lookup_ip_location.definition().operation != LOOKUP_IP_LOCATION_DEFINITION.operation:
            raise ValueError("ALAPI IP Lookup connection operation identity differs")
        self.reference = reference
        self._lookup_ip_location = lookup_ip_location
        self._client = client

    def lookup_ip_location(self) -> Query[LookupIPLocationInput, LookupIPLocationOutput]:
        return self._lookup_ip_location

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "alapi.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("ALAPI IP Lookup connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.alapi.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("ALAPI IP Lookup client is required")
    return Connection(reference, client.lookup_ip_location(), client)

setattr(new_connection, '__module__', 'dex_connectors.alapi.connection')


def new_connection_with_operations(
    lookup_ip_location: Query[LookupIPLocationInput, LookupIPLocationOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, lookup_ip_location)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.alapi.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("ALAPI IP Lookup stored credential does not match the connector credential fields")
        if set(material) - set(('token',)):
            raise ValueError("ALAPI IP Lookup stored credential does not match the connector credential fields")
        token = material.get("token")
        if token is not None and not (isinstance(token, str)):
            raise ValueError("ALAPI IP Lookup credential token has an invalid type")
        if not token:
            raise ValueError("ALAPI IP Lookup credential token is required")
        return Credentials(SecretString(token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("ALAPI IP Lookup connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("ALAPI IP Lookup project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("ALAPI IP Lookup project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.alapi.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("ALAPI IP Lookup configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("ALAPI IP Lookup configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("ALAPI IP Lookup configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
