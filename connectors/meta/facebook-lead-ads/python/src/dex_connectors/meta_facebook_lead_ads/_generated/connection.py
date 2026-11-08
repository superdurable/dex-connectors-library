# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_LEAD_DEFINITION, LIST_LEAD_FORMS_DEFINITION
from ..models import GetLeadInput, LeadFormsPage, LeadMetadata, ListLeadFormsInput

class Connection:

    __slots__ = ('_list_lead_forms', '_get_lead', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_lead_forms: Query[ListLeadFormsInput, LeadFormsPage],
        get_lead: Query[GetLeadInput, LeadMetadata],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_lead_forms is None or get_lead is None:
            raise ValueError("Facebook Lead Ads connection provider and operations are required")
        if list_lead_forms.definition().operation != LIST_LEAD_FORMS_DEFINITION.operation:
            raise ValueError("Facebook Lead Ads connection operation identity differs")
        if get_lead.definition().operation != GET_LEAD_DEFINITION.operation:
            raise ValueError("Facebook Lead Ads connection operation identity differs")
        self.reference = reference
        self._list_lead_forms = list_lead_forms
        self._get_lead = get_lead
        self._client = client

    def list_lead_forms(self) -> Query[ListLeadFormsInput, LeadFormsPage]:
        return self._list_lead_forms

    def get_lead(self) -> Query[GetLeadInput, LeadMetadata]:
        return self._get_lead

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "meta_facebook_lead_ads.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Facebook Lead Ads connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.meta_facebook_lead_ads.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Facebook Lead Ads client is required")
    return Connection(reference, client.list_lead_forms(), client.get_lead(), client)

setattr(new_connection, '__module__', 'dex_connectors.meta_facebook_lead_ads.connection')


def new_connection_with_operations(
    list_lead_forms: Query[ListLeadFormsInput, LeadFormsPage],
    get_lead: Query[GetLeadInput, LeadMetadata],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_lead_forms, get_lead)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.meta_facebook_lead_ads.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Facebook Lead Ads stored credential does not match the connector credential fields")
        if set(material) - set(('page_access_token',)):
            raise ValueError("Facebook Lead Ads stored credential does not match the connector credential fields")
        page_access_token = material.get("page_access_token")
        if page_access_token is not None and not (isinstance(page_access_token, str)):
            raise ValueError("Facebook Lead Ads credential page_access_token has an invalid type")
        if not page_access_token:
            raise ValueError("Facebook Lead Ads credential page_access_token is required")
        return Credentials(SecretString(page_access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Facebook Lead Ads connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Facebook Lead Ads project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Facebook Lead Ads project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.meta_facebook_lead_ads.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Facebook Lead Ads configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Facebook Lead Ads configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Facebook Lead Ads configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Facebook Lead Ads configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
