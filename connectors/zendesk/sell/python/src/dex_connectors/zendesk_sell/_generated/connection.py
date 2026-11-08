# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_LEAD_DEFINITION, LIST_LEADS_DEFINITION
from ..models import GetLeadInput, LeadMetadata, LeadsPage, ListLeadsInput

class Connection:

    __slots__ = ('_list_leads', '_get_lead', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_leads: Query[ListLeadsInput, LeadsPage],
        get_lead: Query[GetLeadInput, LeadMetadata],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_leads is None or get_lead is None:
            raise ValueError("Zendesk Sell connection provider and operations are required")
        if list_leads.definition().operation != LIST_LEADS_DEFINITION.operation:
            raise ValueError("Zendesk Sell connection operation identity differs")
        if get_lead.definition().operation != GET_LEAD_DEFINITION.operation:
            raise ValueError("Zendesk Sell connection operation identity differs")
        self.reference = reference
        self._list_leads = list_leads
        self._get_lead = get_lead
        self._client = client

    def list_leads(self) -> Query[ListLeadsInput, LeadsPage]:
        return self._list_leads

    def get_lead(self) -> Query[GetLeadInput, LeadMetadata]:
        return self._get_lead

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "zendesk_sell.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Zendesk Sell connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.zendesk_sell.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Zendesk Sell client is required")
    return Connection(reference, client.list_leads(), client.get_lead(), client)

setattr(new_connection, '__module__', 'dex_connectors.zendesk_sell.connection')


def new_connection_with_operations(
    list_leads: Query[ListLeadsInput, LeadsPage],
    get_lead: Query[GetLeadInput, LeadMetadata],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_leads, get_lead)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.zendesk_sell.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Zendesk Sell stored credential does not match the connector credential fields")
        if set(material) - set(('access_token',)):
            raise ValueError("Zendesk Sell stored credential does not match the connector credential fields")
        access_token = material.get("access_token")
        if access_token is not None and not (isinstance(access_token, str)):
            raise ValueError("Zendesk Sell credential access_token has an invalid type")
        if not access_token:
            raise ValueError("Zendesk Sell credential access_token is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Zendesk Sell connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Zendesk Sell project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Zendesk Sell project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.zendesk_sell.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Zendesk Sell configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Zendesk Sell configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Zendesk Sell configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Zendesk Sell configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
