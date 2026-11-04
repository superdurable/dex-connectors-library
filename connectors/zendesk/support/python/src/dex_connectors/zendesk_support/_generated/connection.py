# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import (
    CONNECTOR_ID,
    CREATE_TICKET_DEFINITION,
    GET_TICKET_DEFINITION,
    SEARCH_TICKETS_DEFINITION,
    UPDATE_TICKET_DEFINITION,
)
from ..models import (
    CreateTicketInput,
    CreateTicketOutput,
    GetTicketInput,
    SearchTicketsInput,
    SearchTicketsOutput,
    TicketDetails,
    UpdateTicketInput,
    UpdateTicketOutput,
)


class Connection:
    __slots__ = ("_search_tickets", "_get_ticket", "_create_ticket", "_update_ticket", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        search_tickets: Query[SearchTicketsInput, SearchTicketsOutput],
        get_ticket: Query[GetTicketInput, TicketDetails],
        create_ticket: Mutation[CreateTicketInput, CreateTicketOutput],
        update_ticket: Mutation[UpdateTicketInput, UpdateTicketOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if (
            reference.provider != CONNECTOR_ID
            or search_tickets is None
            or get_ticket is None
            or create_ticket is None
            or update_ticket is None
        ):
            raise ValueError("Zendesk Support connection provider and operations are required")
        if search_tickets.definition().operation != SEARCH_TICKETS_DEFINITION.operation:
            raise ValueError("Zendesk Support connection operation identity differs")
        if get_ticket.definition().operation != GET_TICKET_DEFINITION.operation:
            raise ValueError("Zendesk Support connection operation identity differs")
        if create_ticket.definition().operation != CREATE_TICKET_DEFINITION.operation:
            raise ValueError("Zendesk Support connection operation identity differs")
        if update_ticket.definition().operation != UPDATE_TICKET_DEFINITION.operation:
            raise ValueError("Zendesk Support connection operation identity differs")
        self.reference = reference
        self._search_tickets = search_tickets
        self._get_ticket = get_ticket
        self._create_ticket = create_ticket
        self._update_ticket = update_ticket
        self._client = client

    def search_tickets(self) -> Query[SearchTicketsInput, SearchTicketsOutput]:
        return self._search_tickets

    def get_ticket(self) -> Query[GetTicketInput, TicketDetails]:
        return self._get_ticket

    def create_ticket(self) -> Mutation[CreateTicketInput, CreateTicketOutput]:
        return self._create_ticket

    def update_ticket(self) -> Mutation[UpdateTicketInput, UpdateTicketOutput]:
        return self._update_ticket

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "zendesk_support.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Zendesk Support connections cannot be serialized")


setattr(Connection, "__module__", "dex_connectors.zendesk_support.connection")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Zendesk Support client is required")
    return Connection(
        reference,
        client.search_tickets(),
        client.get_ticket(),
        client.create_ticket(),
        client.update_ticket(),
        client,
    )


setattr(new_connection, "__module__", "dex_connectors.zendesk_support.connection")


def new_connection_with_operations(
    search_tickets: Query[SearchTicketsInput, SearchTicketsOutput],
    get_ticket: Query[GetTicketInput, TicketDetails],
    create_ticket: Mutation[CreateTicketInput, CreateTicketOutput],
    update_ticket: Mutation[UpdateTicketInput, UpdateTicketOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_tickets, get_ticket, create_ticket, update_ticket)


setattr(new_connection_with_operations, "__module__", "dex_connectors.zendesk_support.connection")


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        email = material.get("email")
        if not isinstance(email, str) or not email:
            raise ValueError("Zendesk Support credential email is required")
        api_token = material.get("api_token")
        if not isinstance(api_token, str) or not api_token:
            raise ValueError("Zendesk Support credential api_token is required")
        return Credentials(email, SecretString(api_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Zendesk Support connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Zendesk Support project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Zendesk Support project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


setattr(new_project_connection, "__module__", "dex_connectors.zendesk_support.connection")


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"subdomain", "maxResponseBytes"}:
        raise ValueError("Zendesk Support configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "subdomain" in value:
        subdomain = value["subdomain"]
        if not (isinstance(subdomain, str)):
            raise ValueError("Zendesk Support configuration subdomain has an invalid type")
        config_arguments["subdomain"] = subdomain
    else:
        raise ValueError("Zendesk Support configuration subdomain is required")
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Zendesk Support configuration maxResponseBytes has an invalid type")
        config_arguments["max_response_bytes"] = max_response_bytes
    return Config(**config_arguments)
