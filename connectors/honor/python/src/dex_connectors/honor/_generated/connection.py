# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LOOKUP_APP_ID_DEFINITION
from ..models import LookupAppIDInput, LookupAppIDOutput

class Connection:

    __slots__ = ('_lookup_app_id', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        lookup_app_id: Query[LookupAppIDInput, LookupAppIDOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or lookup_app_id is None:
            raise ValueError("HONOR App Market connection provider and operations are required")
        if lookup_app_id.definition().operation != LOOKUP_APP_ID_DEFINITION.operation:
            raise ValueError("HONOR App Market connection operation identity differs")
        self.reference = reference
        self._lookup_app_id = lookup_app_id
        self._client = client

    def lookup_app_id(self) -> Query[LookupAppIDInput, LookupAppIDOutput]:
        return self._lookup_app_id

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "honor.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("HONOR App Market connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.honor.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("HONOR App Market client is required")
    return Connection(reference, client.lookup_app_id(), client)

setattr(new_connection, '__module__', 'dex_connectors.honor.connection')


def new_connection_with_operations(
    lookup_app_id: Query[LookupAppIDInput, LookupAppIDOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, lookup_app_id)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.honor.connection')


class CredentialMaterialError(ValueError):
    "Stored HONOR App Market credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("HONOR App Market stored credential does not match the connector credential fields")
        if set(material) - set(('clientID', 'clientSecret')):
            raise CredentialMaterialError("HONOR App Market stored credential does not match the connector credential fields")
        client_id = material.get("clientID")
        if client_id is not None and not (isinstance(client_id, str)):
            raise CredentialMaterialError("HONOR App Market credential clientID has an invalid type")
        client_secret = material.get("clientSecret")
        if client_secret is not None and not (isinstance(client_secret, str)):
            raise CredentialMaterialError("HONOR App Market credential clientSecret has an invalid type")
        if not client_id:
            raise CredentialMaterialError("HONOR App Market credential clientID is required")
        if not client_secret:
            raise CredentialMaterialError("HONOR App Market credential clientSecret is required")
        return Credentials(SecretString(client_id), SecretString(client_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("HONOR App Market connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("HONOR App Market project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("HONOR App Market project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.honor.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("HONOR App Market configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("HONOR App Market configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("HONOR App Market configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
