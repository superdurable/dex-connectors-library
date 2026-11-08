# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_CAMPAIGNS_DEFINITION
from ..models import CampaignsPage, ListCampaignsInput

class Connection:

    __slots__ = ('_list_campaigns', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_campaigns: Query[ListCampaignsInput, CampaignsPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_campaigns is None:
            raise ValueError("TikTok for Business connection provider and operations are required")
        if list_campaigns.definition().operation != LIST_CAMPAIGNS_DEFINITION.operation:
            raise ValueError("TikTok for Business connection operation identity differs")
        self.reference = reference
        self._list_campaigns = list_campaigns
        self._client = client

    def list_campaigns(self) -> Query[ListCampaignsInput, CampaignsPage]:
        return self._list_campaigns

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "tiktok.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("TikTok for Business connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.tiktok.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("TikTok for Business client is required")
    return Connection(reference, client.list_campaigns(), client)

setattr(new_connection, '__module__', 'dex_connectors.tiktok.connection')


def new_connection_with_operations(
    list_campaigns: Query[ListCampaignsInput, CampaignsPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_campaigns)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.tiktok.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("TikTok for Business stored credential does not match the connector credential fields")
        if set(material) - set(('access_token',)):
            raise ValueError("TikTok for Business stored credential does not match the connector credential fields")
        access_token = material.get("access_token")
        if access_token is not None and not (isinstance(access_token, str)):
            raise ValueError("TikTok for Business credential access_token has an invalid type")
        if not access_token:
            raise ValueError("TikTok for Business credential access_token is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("TikTok for Business connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("TikTok for Business project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("TikTok for Business project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.tiktok.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("TikTok for Business configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("TikTok for Business configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("TikTok for Business configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("TikTok for Business configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
