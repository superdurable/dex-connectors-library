# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_RUNNING_AGENTS_DEFINITION
from ..models import ListRunningAgentsInput, RunningAgents

class Connection:

    __slots__ = ('_list_running_agents', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_running_agents: Query[ListRunningAgentsInput, RunningAgents],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_running_agents is None:
            raise ValueError("Agora Conversational AI connection provider and operations are required")
        if list_running_agents.definition().operation != LIST_RUNNING_AGENTS_DEFINITION.operation:
            raise ValueError("Agora Conversational AI connection operation identity differs")
        self.reference = reference
        self._list_running_agents = list_running_agents
        self._client = client

    def list_running_agents(self) -> Query[ListRunningAgentsInput, RunningAgents]:
        return self._list_running_agents

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "agora.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Agora Conversational AI connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.agora.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Agora Conversational AI client is required")
    return Connection(reference, client.list_running_agents(), client)

setattr(new_connection, '__module__', 'dex_connectors.agora.connection')


def new_connection_with_operations(
    list_running_agents: Query[ListRunningAgentsInput, RunningAgents],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_running_agents)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.agora.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Agora Conversational AI stored credential does not match the connector credential fields")
        if set(material) - set(('appId', 'customerId', 'customerSecret')):
            raise ValueError("Agora Conversational AI stored credential does not match the connector credential fields")
        app_id = material.get("appId")
        if app_id is not None and not (isinstance(app_id, str)):
            raise ValueError("Agora Conversational AI credential appId has an invalid type")
        customer_id = material.get("customerId")
        if customer_id is not None and not (isinstance(customer_id, str)):
            raise ValueError("Agora Conversational AI credential customerId has an invalid type")
        customer_secret = material.get("customerSecret")
        if customer_secret is not None and not (isinstance(customer_secret, str)):
            raise ValueError("Agora Conversational AI credential customerSecret has an invalid type")
        if not app_id:
            raise ValueError("Agora Conversational AI credential appId is required")
        if not customer_id:
            raise ValueError("Agora Conversational AI credential customerId is required")
        if not customer_secret:
            raise ValueError("Agora Conversational AI credential customerSecret is required")
        return Credentials(app_id, customer_id, SecretString(customer_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Agora Conversational AI connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Agora Conversational AI project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Agora Conversational AI project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.agora.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Agora Conversational AI configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Agora Conversational AI configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Agora Conversational AI configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Agora Conversational AI configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
