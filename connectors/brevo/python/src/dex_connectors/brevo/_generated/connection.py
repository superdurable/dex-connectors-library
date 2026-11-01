# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation bindings for one named Dex connection."""

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CREATE_OR_UPDATE_CONTACT_DEFINITION, GET_CONTACT_DEFINITION
from ..models import Contact, ContactWriteResult, CreateOrUpdateContactInput, GetContactInput

class Connection:
    """Nonserializable Brevo operation binding."""

    __slots__ = ('_create_or_update_contact', '_get_contact', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        create_or_update_contact: Mutation[CreateOrUpdateContactInput, ContactWriteResult],
        get_contact: Query[GetContactInput, Contact],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or create_or_update_contact is None or get_contact is None:
            raise ValueError("Brevo connection provider and operations are required")
        if create_or_update_contact.definition().operation != CREATE_OR_UPDATE_CONTACT_DEFINITION.operation:
            raise ValueError("Brevo connection operation identity differs")
        if get_contact.definition().operation != GET_CONTACT_DEFINITION.operation:
            raise ValueError("Brevo connection operation identity differs")
        self.reference = reference
        self._create_or_update_contact = create_or_update_contact
        self._get_contact = get_contact
        self._client = client

    def create_or_update_contact(self) -> Mutation[CreateOrUpdateContactInput, ContactWriteResult]:
        """Return the create_or_update_contact operation for a Dex Step."""
        return self._create_or_update_contact

    def get_contact(self) -> Query[GetContactInput, Contact]:
        """Return the get_contact operation for a Dex Step."""
        return self._get_contact

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "brevo.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Brevo connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.brevo.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a provider client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Brevo client is required")
    return Connection(reference, client.create_or_update_contact(), client.get_contact(), client)

setattr(new_connection, '__module__', 'dex_connectors.brevo.connection')


def new_connection_with_operations(
    create_or_update_contact: Mutation[CreateOrUpdateContactInput, ContactWriteResult],
    get_contact: Query[GetContactInput, Contact],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same logical connection identity."""
    return Connection(reference, create_or_update_contact, get_contact)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.brevo.connection')


class _ProjectCredentialSource:
    """Resolve current secret material at each provider attempt."""

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("Brevo credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared project settings and resolve provider credentials per call."""
    if project is None:
        raise ValueError("Brevo connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Brevo project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Brevo project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.brevo.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Brevo configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("Brevo configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("Brevo configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("Brevo configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
