# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString, CredentialRefreshDriver
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, PROVIDER, CREATE_EVENT_DEFINITION, GET_EVENT_DEFINITION, LIST_EVENTS_DEFINITION, QUERY_FREE_BUSY_DEFINITION, UPDATE_EVENT_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import CreateEventInput, CreateEventOutput, Event, GetEventInput, ListEventsInput, ListEventsOutput, QueryFreeBusyInput, QueryFreeBusyOutput, UpdateEventInput, UpdateEventOutput

class Connection:

    __slots__ = ('_list_events', '_get_event', '_create_event', '_update_event', '_query_free_busy', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_events: Query[ListEventsInput, ListEventsOutput],
        get_event: Query[GetEventInput, Event],
        create_event: Mutation[CreateEventInput, CreateEventOutput],
        update_event: Mutation[UpdateEventInput, UpdateEventOutput],
        query_free_busy: Query[QueryFreeBusyInput, QueryFreeBusyOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or list_events is None or get_event is None or create_event is None or update_event is None or query_free_busy is None:
            raise ValueError("Microsoft Outlook Calendar connection provider and operations are required")
        if list_events.definition().operation != LIST_EVENTS_DEFINITION.operation:
            raise ValueError("Microsoft Outlook Calendar connection operation identity differs")
        if get_event.definition().operation != GET_EVENT_DEFINITION.operation:
            raise ValueError("Microsoft Outlook Calendar connection operation identity differs")
        if create_event.definition().operation != CREATE_EVENT_DEFINITION.operation:
            raise ValueError("Microsoft Outlook Calendar connection operation identity differs")
        if update_event.definition().operation != UPDATE_EVENT_DEFINITION.operation:
            raise ValueError("Microsoft Outlook Calendar connection operation identity differs")
        if query_free_busy.definition().operation != QUERY_FREE_BUSY_DEFINITION.operation:
            raise ValueError("Microsoft Outlook Calendar connection operation identity differs")
        self.reference = reference
        self._list_events = list_events
        self._get_event = get_event
        self._create_event = create_event
        self._update_event = update_event
        self._query_free_busy = query_free_busy
        self._client = client

    def list_events(self) -> Query[ListEventsInput, ListEventsOutput]:
        return self._list_events

    def get_event(self) -> Query[GetEventInput, Event]:
        return self._get_event

    def create_event(self) -> Mutation[CreateEventInput, CreateEventOutput]:
        return self._create_event

    def update_event(self) -> Mutation[UpdateEventInput, UpdateEventOutput]:
        return self._update_event

    def query_free_busy(self) -> Query[QueryFreeBusyInput, QueryFreeBusyOutput]:
        return self._query_free_busy

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "microsoft_outlook_calendar.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Microsoft Outlook Calendar connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.microsoft_outlook_calendar.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Microsoft Outlook Calendar client is required")
    return Connection(reference, client.list_events(), client.get_event(), client.create_event(), client.update_event(), client.query_free_busy(), client)

setattr(new_connection, '__module__', 'dex_connectors.microsoft_outlook_calendar.connection')


def new_connection_with_operations(
    list_events: Query[ListEventsInput, ListEventsOutput],
    get_event: Query[GetEventInput, Event],
    create_event: Mutation[CreateEventInput, CreateEventOutput],
    update_event: Mutation[UpdateEventInput, UpdateEventOutput],
    query_free_busy: Query[QueryFreeBusyInput, QueryFreeBusyOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_events, get_event, create_event, update_event, query_free_busy)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.microsoft_outlook_calendar.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Microsoft Outlook Calendar stored credential does not match the connector credential fields")
    if set(material) - set(('client_id', 'client_secret', 'access_token', 'refresh_token', 'auth_method')):
        raise ValueError("Microsoft Outlook Calendar stored credential does not match the connector credential fields")
    is_shared = shared
    client_id = material.get("client_id")
    if client_id is not None and not (isinstance(client_id, str)):
        raise ValueError("Microsoft Outlook Calendar credential client_id has an invalid type")
    client_secret = material.get("client_secret")
    if client_secret is not None and not (isinstance(client_secret, str)):
        raise ValueError("Microsoft Outlook Calendar credential client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Microsoft Outlook Calendar credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Microsoft Outlook Calendar credential refresh_token has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Microsoft Outlook Calendar credential auth_method is invalid")
    method = selected_method or "microsoft-oauth"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "microsoft-oauth":
        if not client_id:
            raise ValueError("Microsoft Outlook Calendar credential client_id is required")
        if client_id:
            values["client_id"] = client_id
        if not client_secret and not is_shared:
            raise ValueError("Microsoft Outlook Calendar credential client_secret is required")
        if client_secret:
            values["client_secret"] = SecretString(client_secret)
        if not access_token:
            raise ValueError("Microsoft Outlook Calendar credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Microsoft Outlook Calendar credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    elif method == "app-only":
        if not client_id:
            raise ValueError("Microsoft Outlook Calendar credential client_id is required")
        if client_id:
            values["client_id"] = client_id
        if not client_secret and not is_shared:
            raise ValueError("Microsoft Outlook Calendar credential client_secret is required")
        if client_secret:
            values["client_secret"] = SecretString(client_secret)
        if not access_token and is_shared:
            raise ValueError("Microsoft Outlook Calendar credential access_token is required")
        values["access_token"] = SecretString(access_token or '')
    else:
        raise ValueError("Microsoft Outlook Calendar credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "microsoft-oauth"
    material['auth_method'] = method
    if method == "microsoft-oauth":
        if credentials.client_id:
            material["client_id"] = credentials.client_id
        if (credentials.client_secret.reveal() if credentials.client_secret is not None else ''):
            material["client_secret"] = (credentials.client_secret.reveal() if credentials.client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
    elif method == "app-only":
        if credentials.client_id:
            material["client_id"] = credentials.client_id
        if (credentials.client_secret.reveal() if credentials.client_secret is not None else ''):
            material["client_secret"] = (credentials.client_secret.reveal() if credentials.client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
    else:
        raise ValueError("Microsoft Outlook Calendar credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Microsoft Outlook Calendar", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

    async def resolve(self, call: Call) -> Credentials:
        return await self.provider.resolve_with_refresh(call, self.driver)

    async def resolve_with_refresh(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_with_refresh(call, driver or self.driver)

    async def resolve_after_rejection(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_after_rejection(call, driver or self.driver)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Microsoft Outlook Calendar connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Microsoft Outlook Calendar project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Microsoft Outlook Calendar project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.microsoft_outlook_calendar.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes", "tenantId", "mailbox"}:
        raise ValueError("Microsoft Outlook Calendar configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Microsoft Outlook Calendar configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "tenantId" in value:
        tenant_id = value["tenantId"]
        if not (isinstance(tenant_id, str)):
            raise ValueError("Microsoft Outlook Calendar configuration tenantId has an invalid type")
        config_arguments['tenant_id'] = tenant_id
    else:
        config_arguments['tenant_id'] = ''
    if "mailbox" in value:
        mailbox = value["mailbox"]
        if not (isinstance(mailbox, str)):
            raise ValueError("Microsoft Outlook Calendar configuration mailbox has an invalid type")
        config_arguments['mailbox'] = mailbox
    else:
        config_arguments['mailbox'] = ''
    return Config(**config_arguments)
