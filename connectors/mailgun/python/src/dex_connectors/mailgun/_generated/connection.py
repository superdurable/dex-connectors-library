# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_EVENTS_DEFINITION, LIST_DOMAINS_DEFINITION
from ..models import DomainsOutput, EventsOutput, GetEventsInput, ListDomainsInput

class Connection:

    __slots__ = ('_get_events', '_list_domains', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_events: Query[GetEventsInput, EventsOutput],
        list_domains: Query[ListDomainsInput, DomainsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_events is None or list_domains is None:
            raise ValueError("Mailgun connection provider and operations are required")
        if get_events.definition().operation != GET_EVENTS_DEFINITION.operation:
            raise ValueError("Mailgun connection operation identity differs")
        if list_domains.definition().operation != LIST_DOMAINS_DEFINITION.operation:
            raise ValueError("Mailgun connection operation identity differs")
        self.reference = reference
        self._get_events = get_events
        self._list_domains = list_domains
        self._client = client

    def get_events(self) -> Query[GetEventsInput, EventsOutput]:
        return self._get_events

    def list_domains(self) -> Query[ListDomainsInput, DomainsOutput]:
        return self._list_domains

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "mailgun.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Mailgun connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.mailgun.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Mailgun client is required")
    return Connection(reference, client.get_events(), client.list_domains(), client)

setattr(new_connection, '__module__', 'dex_connectors.mailgun.connection')


def new_connection_with_operations(
    get_events: Query[GetEventsInput, EventsOutput],
    list_domains: Query[ListDomainsInput, DomainsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_events, list_domains)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.mailgun.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("Mailgun credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Mailgun connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Mailgun project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Mailgun project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.mailgun.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Mailgun configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Mailgun configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Mailgun configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Mailgun configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
