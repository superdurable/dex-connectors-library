# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GENERATE_TEXT_DEFINITION
from ..models import GenerateTextRequest, GenerateTextResponse

class Connection:

    __slots__ = ('_generate_text', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        generate_text: Query[GenerateTextRequest, GenerateTextResponse],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or generate_text is None:
            raise ValueError("LLM connection provider and operations are required")
        if generate_text.definition().operation != GENERATE_TEXT_DEFINITION.operation:
            raise ValueError("LLM connection operation identity differs")
        self.reference = reference
        self._generate_text = generate_text
        self._client = client

    def generate_text(self) -> Query[GenerateTextRequest, GenerateTextResponse]:
        return self._generate_text

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "superdurable_llm.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("LLM connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.superdurable_llm.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("LLM client is required")
    return Connection(reference, client.generate_text(), client)

setattr(new_connection, '__module__', 'dex_connectors.superdurable_llm.connection')


def new_connection_with_operations(
    generate_text: Query[GenerateTextRequest, GenerateTextResponse],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, generate_text)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.superdurable_llm.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("LLM credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("LLM connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("LLM project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("LLM project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.superdurable_llm.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"provider", "baseUrl", "model", "region", "anthropicWorkspaceId", "maxResponseBytes"}:
        raise ValueError("LLM configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "provider" in value:
        provider = value["provider"]
        if not (isinstance(provider, str) and provider in ('openai', 'openai-compatible', 'anthropic', 'gemini', 'qwen', 'deepseek', 'meta', 'mistral', 'kimi', 'xai')):
            raise ValueError("LLM configuration provider has an invalid type")
        config_arguments['provider'] = provider
    else:
        raise ValueError("LLM configuration provider is required")
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("LLM configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    else:
        config_arguments['base_url'] = ''
    if "model" in value:
        model = value["model"]
        if not (isinstance(model, str)):
            raise ValueError("LLM configuration model has an invalid type")
        config_arguments['model'] = model
    else:
        config_arguments['model'] = ''
    if "region" in value:
        region = value["region"]
        if not (isinstance(region, str) and region in ('global', 'us', 'eu', 'china', 'hong-kong')):
            raise ValueError("LLM configuration region has an invalid type")
        config_arguments['region'] = region
    if "anthropicWorkspaceId" in value:
        anthropic_workspace_id = value["anthropicWorkspaceId"]
        if not (isinstance(anthropic_workspace_id, str)):
            raise ValueError("LLM configuration anthropicWorkspaceId has an invalid type")
        config_arguments['anthropic_workspace_id'] = anthropic_workspace_id
    else:
        config_arguments['anthropic_workspace_id'] = ''
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("LLM configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    else:
        config_arguments['max_response_bytes'] = 0
    return Config(**config_arguments)
