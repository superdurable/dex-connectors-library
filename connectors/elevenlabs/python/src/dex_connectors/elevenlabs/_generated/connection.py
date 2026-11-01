# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation bindings for one named Dex connection."""

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CREATE_SPEECH_DEFINITION, LIST_VOICES_DEFINITION
from ..models import CreateSpeechInput, CreateSpeechOutput, ListVoicesInput, ListVoicesOutput

class Connection:
    """Nonserializable ElevenLabs operation binding."""

    __slots__ = ('_list_voices', '_create_speech', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_voices: Query[ListVoicesInput, ListVoicesOutput],
        create_speech: Query[CreateSpeechInput, CreateSpeechOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_voices is None or create_speech is None:
            raise ValueError("ElevenLabs connection provider and operations are required")
        if list_voices.definition().operation != LIST_VOICES_DEFINITION.operation:
            raise ValueError("ElevenLabs connection operation identity differs")
        if create_speech.definition().operation != CREATE_SPEECH_DEFINITION.operation:
            raise ValueError("ElevenLabs connection operation identity differs")
        self.reference = reference
        self._list_voices = list_voices
        self._create_speech = create_speech
        self._client = client

    def list_voices(self) -> Query[ListVoicesInput, ListVoicesOutput]:
        """Return the list_voices operation for a Dex Step."""
        return self._list_voices

    def create_speech(self) -> Query[CreateSpeechInput, CreateSpeechOutput]:
        """Return the create_speech operation for a Dex Step."""
        return self._create_speech

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "elevenlabs.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("ElevenLabs connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.elevenlabs.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a provider client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("ElevenLabs client is required")
    return Connection(reference, client.list_voices(), client.create_speech(), client)

setattr(new_connection, '__module__', 'dex_connectors.elevenlabs.connection')


def new_connection_with_operations(
    list_voices: Query[ListVoicesInput, ListVoicesOutput],
    create_speech: Query[CreateSpeechInput, CreateSpeechOutput],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same logical connection identity."""
    return Connection(reference, list_voices, create_speech)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.elevenlabs.connection')


class _ProjectCredentialSource:
    """Resolve current secret material at each provider attempt."""

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("ElevenLabs credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared project settings and resolve provider credentials per call."""
    if project is None:
        raise ValueError("ElevenLabs connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("ElevenLabs project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("ElevenLabs project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.elevenlabs.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("ElevenLabs configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("ElevenLabs configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("ElevenLabs configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("ElevenLabs configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
