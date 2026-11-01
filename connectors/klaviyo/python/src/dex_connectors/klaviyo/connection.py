# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# connectorctl: handwritten facade
"""Klaviyo connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Mutation, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import CONNECTOR_ID, CREATE_EVENT_DEFINITION, GET_PROFILE_DEFINITION, Client, Config, Credentials
from .models import CreateEventInput, EventAcceptance, GetProfileInput, Profile


class Connection:
    """Nonserializable Klaviyo operation bindings for a named project connection."""

    __slots__ = ("_create_event", "_get_profile", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        create_event: Mutation[CreateEventInput, EventAcceptance],
        get_profile: Query[GetProfileInput, Profile],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or create_event is None or get_profile is None:
            raise ValueError("Klaviyo connection provider and operations are required")
        if (
            create_event.definition().operation != CREATE_EVENT_DEFINITION.operation
            or get_profile.definition().operation != GET_PROFILE_DEFINITION.operation
        ):
            raise ValueError("Klaviyo connection operation identity differs")
        self.reference = reference
        self._create_event = create_event
        self._get_profile = get_profile
        self._client = client

    def create_event(self) -> Mutation[CreateEventInput, EventAcceptance]:
        """Return the event Mutation used by an application Step."""
        return self._create_event

    def get_profile(self) -> Query[GetProfileInput, Profile]:
        """Return the profile Query used by an application Step."""
        return self._get_profile

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "klaviyo.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Klaviyo connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Klaviyo client is required")
    return Connection(reference, client.create_event(), client.get_profile(), client)


def new_connection_with_operations(
    create_event: Mutation[CreateEventInput, EventAcceptance],
    get_profile: Query[GetProfileInput, Profile],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, create_event, get_profile)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("private_api_key")
        if not isinstance(value, str) or not value:
            raise ValueError("Klaviyo credential private_api_key is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current private key per call."""
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


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Klaviyo configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Klaviyo configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Klaviyo configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
