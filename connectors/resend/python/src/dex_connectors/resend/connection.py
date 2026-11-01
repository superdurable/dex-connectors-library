# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Resend connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Mutation, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import CONNECTOR_ID, GET_EMAIL_DEFINITION, SEND_EMAIL_DEFINITION, Client, Config, Credentials
from .models import Email, GetEmailInput, SendEmailInput, SentEmail


class Connection:
    """Nonserializable Resend operation bindings for a named project connection."""

    __slots__ = ("_send_email", "_get_email", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        send_email: Mutation[SendEmailInput, SentEmail],
        get_email: Query[GetEmailInput, Email],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or send_email is None or get_email is None:
            raise ValueError("Resend connection provider and operations are required")
        if (
            send_email.definition().operation != SEND_EMAIL_DEFINITION.operation
            or get_email.definition().operation != GET_EMAIL_DEFINITION.operation
        ):
            raise ValueError("Resend connection operation identity differs")
        self.reference = reference
        self._send_email = send_email
        self._get_email = get_email
        self._client = client

    def send_email(self) -> Mutation[SendEmailInput, SentEmail]:
        """Return the send Mutation used by an application Step."""
        return self._send_email

    def get_email(self) -> Query[GetEmailInput, Email]:
        """Return the lookup Query used by an application Step."""
        return self._get_email

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "resend.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Resend connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Resend client is required")
    return Connection(reference, client.send_email(), client.get_email(), client)


def new_connection_with_operations(
    send_email: Mutation[SendEmailInput, SentEmail],
    get_email: Query[GetEmailInput, Email],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, send_email, get_email)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("api_key")
        if not isinstance(value, str) or not value:
            raise ValueError("Resend credential api_key is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current private key per call."""
    if project is None:
        raise ValueError("Resend connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Resend project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Resend project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Resend configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Resend configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Resend configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
