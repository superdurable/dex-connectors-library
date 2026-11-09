# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_ATTENDEE_DEFINITION, LIST_EVENTS_DEFINITION, LIST_ORGANIZATIONS_DEFINITION
from ..models import AttendeeMetadata, EventsPage, GetAttendeeInput, ListEventsInput, ListOrganizationsInput, OrganizationsPage

class Connection:

    __slots__ = ('_list_organizations', '_list_events', '_get_attendee', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_organizations: Query[ListOrganizationsInput, OrganizationsPage],
        list_events: Query[ListEventsInput, EventsPage],
        get_attendee: Query[GetAttendeeInput, AttendeeMetadata],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_organizations is None or list_events is None or get_attendee is None:
            raise ValueError("Eventbrite connection provider and operations are required")
        if list_organizations.definition().operation != LIST_ORGANIZATIONS_DEFINITION.operation:
            raise ValueError("Eventbrite connection operation identity differs")
        if list_events.definition().operation != LIST_EVENTS_DEFINITION.operation:
            raise ValueError("Eventbrite connection operation identity differs")
        if get_attendee.definition().operation != GET_ATTENDEE_DEFINITION.operation:
            raise ValueError("Eventbrite connection operation identity differs")
        self.reference = reference
        self._list_organizations = list_organizations
        self._list_events = list_events
        self._get_attendee = get_attendee
        self._client = client

    def list_organizations(self) -> Query[ListOrganizationsInput, OrganizationsPage]:
        return self._list_organizations

    def list_events(self) -> Query[ListEventsInput, EventsPage]:
        return self._list_events

    def get_attendee(self) -> Query[GetAttendeeInput, AttendeeMetadata]:
        return self._get_attendee

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "eventbrite.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Eventbrite connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.eventbrite.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Eventbrite client is required")
    return Connection(reference, client.list_organizations(), client.list_events(), client.get_attendee(), client)

setattr(new_connection, '__module__', 'dex_connectors.eventbrite.connection')


def new_connection_with_operations(
    list_organizations: Query[ListOrganizationsInput, OrganizationsPage],
    list_events: Query[ListEventsInput, EventsPage],
    get_attendee: Query[GetAttendeeInput, AttendeeMetadata],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_organizations, list_events, get_attendee)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.eventbrite.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Eventbrite stored credential does not match the connector credential fields")
        if set(material) - set(('private_token',)):
            raise ValueError("Eventbrite stored credential does not match the connector credential fields")
        private_token = material.get("private_token")
        if private_token is not None and not (isinstance(private_token, str)):
            raise ValueError("Eventbrite credential private_token has an invalid type")
        if not private_token:
            raise ValueError("Eventbrite credential private_token is required")
        return Credentials(SecretString(private_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Eventbrite connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Eventbrite project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Eventbrite project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.eventbrite.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Eventbrite configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Eventbrite configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Eventbrite configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Eventbrite configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
