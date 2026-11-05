# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, ADD_NOTE_DEFINITION, CREATE_TICKET_DEFINITION, GET_TICKET_DEFINITION, SEARCH_TICKETS_DEFINITION, UPDATE_TICKET_DEFINITION
from ..models import AddNoteInput, AddNoteOutput, CreateTicketInput, CreateTicketOutput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, TicketDetails, UpdateTicketInput, UpdateTicketOutput

class Connection:

    __slots__ = ('_search_tickets', '_get_ticket', '_create_ticket', '_update_ticket', '_add_note', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_tickets: Query[SearchTicketsInput, SearchTicketsOutput],
        get_ticket: Query[GetTicketInput, TicketDetails],
        create_ticket: Mutation[CreateTicketInput, CreateTicketOutput],
        update_ticket: Mutation[UpdateTicketInput, UpdateTicketOutput],
        add_note: Mutation[AddNoteInput, AddNoteOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or search_tickets is None or get_ticket is None or create_ticket is None or update_ticket is None or add_note is None:
            raise ValueError("Freshdesk connection provider and operations are required")
        if search_tickets.definition().operation != SEARCH_TICKETS_DEFINITION.operation:
            raise ValueError("Freshdesk connection operation identity differs")
        if get_ticket.definition().operation != GET_TICKET_DEFINITION.operation:
            raise ValueError("Freshdesk connection operation identity differs")
        if create_ticket.definition().operation != CREATE_TICKET_DEFINITION.operation:
            raise ValueError("Freshdesk connection operation identity differs")
        if update_ticket.definition().operation != UPDATE_TICKET_DEFINITION.operation:
            raise ValueError("Freshdesk connection operation identity differs")
        if add_note.definition().operation != ADD_NOTE_DEFINITION.operation:
            raise ValueError("Freshdesk connection operation identity differs")
        self.reference = reference
        self._search_tickets = search_tickets
        self._get_ticket = get_ticket
        self._create_ticket = create_ticket
        self._update_ticket = update_ticket
        self._add_note = add_note
        self._client = client

    def search_tickets(self) -> Query[SearchTicketsInput, SearchTicketsOutput]:
        return self._search_tickets

    def get_ticket(self) -> Query[GetTicketInput, TicketDetails]:
        return self._get_ticket

    def create_ticket(self) -> Mutation[CreateTicketInput, CreateTicketOutput]:
        return self._create_ticket

    def update_ticket(self) -> Mutation[UpdateTicketInput, UpdateTicketOutput]:
        return self._update_ticket

    def add_note(self) -> Mutation[AddNoteInput, AddNoteOutput]:
        return self._add_note

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "freshworks_freshdesk.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Freshdesk connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.freshworks_freshdesk.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Freshdesk client is required")
    return Connection(reference, client.search_tickets(), client.get_ticket(), client.create_ticket(), client.update_ticket(), client.add_note(), client)

setattr(new_connection, '__module__', 'dex_connectors.freshworks_freshdesk.connection')


def new_connection_with_operations(
    search_tickets: Query[SearchTicketsInput, SearchTicketsOutput],
    get_ticket: Query[GetTicketInput, TicketDetails],
    create_ticket: Mutation[CreateTicketInput, CreateTicketOutput],
    update_ticket: Mutation[UpdateTicketInput, UpdateTicketOutput],
    add_note: Mutation[AddNoteInput, AddNoteOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_tickets, get_ticket, create_ticket, update_ticket, add_note)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.freshworks_freshdesk.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Freshdesk stored credential does not match the connector credential fields")
        if set(material) - set(('api_key',)):
            raise ValueError("Freshdesk stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("Freshdesk credential api_key has an invalid type")
        if not api_key:
            raise ValueError("Freshdesk credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Freshdesk connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Freshdesk project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Freshdesk project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.freshworks_freshdesk.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"domain", "maxResponseBytes"}:
        raise ValueError("Freshdesk configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "domain" in value:
        domain = value["domain"]
        if not (isinstance(domain, str)):
            raise ValueError("Freshdesk configuration domain has an invalid type")
        config_arguments['domain'] = domain
    else:
        raise ValueError("Freshdesk configuration domain is required")
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Freshdesk configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
