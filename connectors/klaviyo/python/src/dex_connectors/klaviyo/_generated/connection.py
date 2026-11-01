# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CREATE_EVENT_DEFINITION, GET_PROFILE_DEFINITION
from ..models import CreateEventInput, EventAcceptance, GetProfileInput, Profile

class Connection:

    __slots__ = ('_create_event', '_get_profile', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        create_event: Mutation[CreateEventInput, EventAcceptance],
        get_profile: Query[GetProfileInput, Profile],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or create_event is None or get_profile is None:
            raise ValueError("Klaviyo connection provider and operations are required")
        if create_event.definition().operation != CREATE_EVENT_DEFINITION.operation:
            raise ValueError("Klaviyo connection operation identity differs")
        if get_profile.definition().operation != GET_PROFILE_DEFINITION.operation:
            raise ValueError("Klaviyo connection operation identity differs")
        self.reference = reference
        self._create_event = create_event
        self._get_profile = get_profile
        self._client = client

    def create_event(self) -> Mutation[CreateEventInput, EventAcceptance]:
        return self._create_event

    def get_profile(self) -> Query[GetProfileInput, Profile]:
        return self._get_profile

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "klaviyo.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Klaviyo connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.klaviyo.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Klaviyo client is required")
    return Connection(reference, client.create_event(), client.get_profile(), client)

setattr(new_connection, '__module__', 'dex_connectors.klaviyo.connection')


def new_connection_with_operations(
    create_event: Mutation[CreateEventInput, EventAcceptance],
    get_profile: Query[GetProfileInput, Profile],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, create_event, get_profile)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.klaviyo.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        private_api_key = material.get("private_api_key")
        if not isinstance(private_api_key, str) or not private_api_key:
            raise ValueError("Klaviyo credential private_api_key is required")
        return Credentials(SecretString(private_api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Klaviyo connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Klaviyo project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Klaviyo project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.klaviyo.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Klaviyo configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("Klaviyo configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("Klaviyo configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("Klaviyo configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
