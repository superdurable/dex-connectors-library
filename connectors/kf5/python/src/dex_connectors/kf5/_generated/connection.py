# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_TICKET_DEFINITION
from ..models import GetTicketInput, GetTicketOutput

class Connection:

    __slots__ = ('_get_ticket', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_ticket: Query[GetTicketInput, GetTicketOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_ticket is None:
            raise ValueError("KF5 Ticket Read connection provider and operations are required")
        if get_ticket.definition().operation != GET_TICKET_DEFINITION.operation:
            raise ValueError("KF5 Ticket Read connection operation identity differs")
        self.reference = reference
        self._get_ticket = get_ticket
        self._client = client

    def get_ticket(self) -> Query[GetTicketInput, GetTicketOutput]:
        return self._get_ticket

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "kf5.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("KF5 Ticket Read connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.kf5.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("KF5 Ticket Read client is required")
    return Connection(reference, client.get_ticket(), client)

setattr(new_connection, '__module__', 'dex_connectors.kf5.connection')


def new_connection_with_operations(
    get_ticket: Query[GetTicketInput, GetTicketOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_ticket)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.kf5.connection')


class CredentialMaterialError(ValueError):
    "Stored KF5 Ticket Read credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("KF5 Ticket Read stored credential does not match the connector credential fields")
        if set(material) - set(('subdomain', 'email', 'apiToken')):
            raise CredentialMaterialError("KF5 Ticket Read stored credential does not match the connector credential fields")
        subdomain = material.get("subdomain")
        if subdomain is not None and not (isinstance(subdomain, str)):
            raise CredentialMaterialError("KF5 Ticket Read credential subdomain has an invalid type")
        email = material.get("email")
        if email is not None and not (isinstance(email, str)):
            raise CredentialMaterialError("KF5 Ticket Read credential email has an invalid type")
        api_token = material.get("apiToken")
        if api_token is not None and not (isinstance(api_token, str)):
            raise CredentialMaterialError("KF5 Ticket Read credential apiToken has an invalid type")
        if not subdomain:
            raise CredentialMaterialError("KF5 Ticket Read credential subdomain is required")
        if not email:
            raise CredentialMaterialError("KF5 Ticket Read credential email is required")
        if not api_token:
            raise CredentialMaterialError("KF5 Ticket Read credential apiToken is required")
        return Credentials(subdomain, SecretString(email), SecretString(api_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("KF5 Ticket Read connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("KF5 Ticket Read project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("KF5 Ticket Read project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.kf5.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("KF5 Ticket Read configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("KF5 Ticket Read configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("KF5 Ticket Read configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
