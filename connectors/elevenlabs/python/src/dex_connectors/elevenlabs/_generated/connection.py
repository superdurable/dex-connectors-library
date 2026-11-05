# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CREATE_SPEECH_DEFINITION, LIST_VOICES_DEFINITION
from ..models import CreateSpeechInput, CreateSpeechOutput, ListVoicesInput, ListVoicesOutput

class Connection:

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
        return self._list_voices

    def create_speech(self) -> Query[CreateSpeechInput, CreateSpeechOutput]:
        return self._create_speech

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "elevenlabs.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("ElevenLabs connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.elevenlabs.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("ElevenLabs client is required")
    return Connection(reference, client.list_voices(), client.create_speech(), client)

setattr(new_connection, '__module__', 'dex_connectors.elevenlabs.connection')


def new_connection_with_operations(
    list_voices: Query[ListVoicesInput, ListVoicesOutput],
    create_speech: Query[CreateSpeechInput, CreateSpeechOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_voices, create_speech)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.elevenlabs.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("ElevenLabs stored credential does not match the connector credential fields")
        if set(material) - set(('api_key',)):
            raise ValueError("ElevenLabs stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("ElevenLabs credential api_key has an invalid type")
        if not api_key:
            raise ValueError("ElevenLabs credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
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
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("ElevenLabs configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("ElevenLabs configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("ElevenLabs configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
