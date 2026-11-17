# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_KNOWLEDGE_BASES_DEFINITION
from ..models import ListKnowledgeBasesInput, ListKnowledgeBasesOutput

class Connection:

    __slots__ = ('_list_knowledge_bases', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_knowledge_bases: Query[ListKnowledgeBasesInput, ListKnowledgeBasesOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_knowledge_bases is None:
            raise ValueError("CTYun connection provider and operations are required")
        if list_knowledge_bases.definition().operation != LIST_KNOWLEDGE_BASES_DEFINITION.operation:
            raise ValueError("CTYun connection operation identity differs")
        self.reference = reference
        self._list_knowledge_bases = list_knowledge_bases
        self._client = client

    def list_knowledge_bases(self) -> Query[ListKnowledgeBasesInput, ListKnowledgeBasesOutput]:
        return self._list_knowledge_bases

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "ctyun.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("CTYun connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.ctyun.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("CTYun client is required")
    return Connection(reference, client.list_knowledge_bases(), client)

setattr(new_connection, '__module__', 'dex_connectors.ctyun.connection')


def new_connection_with_operations(
    list_knowledge_bases: Query[ListKnowledgeBasesInput, ListKnowledgeBasesOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_knowledge_bases)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.ctyun.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("CTYun stored credential does not match the connector credential fields")
        if set(material) - set(('access_key', 'security_key', 'tenant_id')):
            raise ValueError("CTYun stored credential does not match the connector credential fields")
        access_key = material.get("access_key")
        if access_key is not None and not (isinstance(access_key, str)):
            raise ValueError("CTYun credential access_key has an invalid type")
        security_key = material.get("security_key")
        if security_key is not None and not (isinstance(security_key, str)):
            raise ValueError("CTYun credential security_key has an invalid type")
        tenant_id = material.get("tenant_id")
        if tenant_id is not None and not (isinstance(tenant_id, str)):
            raise ValueError("CTYun credential tenant_id has an invalid type")
        if not access_key:
            raise ValueError("CTYun credential access_key is required")
        if not security_key:
            raise ValueError("CTYun credential security_key is required")
        if not tenant_id:
            raise ValueError("CTYun credential tenant_id is required")
        return Credentials(SecretString(access_key), SecretString(security_key), SecretString(tenant_id))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("CTYun connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("CTYun project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("CTYun project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.ctyun.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("CTYun configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("CTYun configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("CTYun configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("CTYun configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
