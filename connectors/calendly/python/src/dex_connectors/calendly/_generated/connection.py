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
from .definitions import CONNECTOR_ID, CANCEL_SCHEDULED_EVENT_DEFINITION, CREATE_SCHEDULING_LINK_DEFINITION, CREATE_WEBHOOK_SUBSCRIPTION_DEFINITION, GET_SCHEDULED_EVENT_DEFINITION, LIST_EVENT_INVITEES_DEFINITION, LIST_SCHEDULED_EVENTS_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import CancelScheduledEventInput, CreateSchedulingLinkInput, CreateWebhookSubscriptionInput, EventInviteePage, GetScheduledEventInput, ListEventInviteesInput, ListScheduledEventsInput, ScheduledEvent, ScheduledEventCancellation, ScheduledEventPage, SchedulingLink, WebhookSubscription

class Connection:

    __slots__ = ('_list_scheduled_events', '_get_scheduled_event', '_list_event_invitees', '_cancel_scheduled_event', '_create_scheduling_link', '_create_webhook_subscription', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_scheduled_events: Query[ListScheduledEventsInput, ScheduledEventPage],
        get_scheduled_event: Query[GetScheduledEventInput, ScheduledEvent],
        list_event_invitees: Query[ListEventInviteesInput, EventInviteePage],
        cancel_scheduled_event: Mutation[CancelScheduledEventInput, ScheduledEventCancellation],
        create_scheduling_link: Mutation[CreateSchedulingLinkInput, SchedulingLink],
        create_webhook_subscription: Mutation[CreateWebhookSubscriptionInput, WebhookSubscription],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_scheduled_events is None or get_scheduled_event is None or list_event_invitees is None or cancel_scheduled_event is None or create_scheduling_link is None or create_webhook_subscription is None:
            raise ValueError("Calendly connection provider and operations are required")
        if list_scheduled_events.definition().operation != LIST_SCHEDULED_EVENTS_DEFINITION.operation:
            raise ValueError("Calendly connection operation identity differs")
        if get_scheduled_event.definition().operation != GET_SCHEDULED_EVENT_DEFINITION.operation:
            raise ValueError("Calendly connection operation identity differs")
        if list_event_invitees.definition().operation != LIST_EVENT_INVITEES_DEFINITION.operation:
            raise ValueError("Calendly connection operation identity differs")
        if cancel_scheduled_event.definition().operation != CANCEL_SCHEDULED_EVENT_DEFINITION.operation:
            raise ValueError("Calendly connection operation identity differs")
        if create_scheduling_link.definition().operation != CREATE_SCHEDULING_LINK_DEFINITION.operation:
            raise ValueError("Calendly connection operation identity differs")
        if create_webhook_subscription.definition().operation != CREATE_WEBHOOK_SUBSCRIPTION_DEFINITION.operation:
            raise ValueError("Calendly connection operation identity differs")
        self.reference = reference
        self._list_scheduled_events = list_scheduled_events
        self._get_scheduled_event = get_scheduled_event
        self._list_event_invitees = list_event_invitees
        self._cancel_scheduled_event = cancel_scheduled_event
        self._create_scheduling_link = create_scheduling_link
        self._create_webhook_subscription = create_webhook_subscription
        self._client = client

    def list_scheduled_events(self) -> Query[ListScheduledEventsInput, ScheduledEventPage]:
        return self._list_scheduled_events

    def get_scheduled_event(self) -> Query[GetScheduledEventInput, ScheduledEvent]:
        return self._get_scheduled_event

    def list_event_invitees(self) -> Query[ListEventInviteesInput, EventInviteePage]:
        return self._list_event_invitees

    def cancel_scheduled_event(self) -> Mutation[CancelScheduledEventInput, ScheduledEventCancellation]:
        return self._cancel_scheduled_event

    def create_scheduling_link(self) -> Mutation[CreateSchedulingLinkInput, SchedulingLink]:
        return self._create_scheduling_link

    def create_webhook_subscription(self) -> Mutation[CreateWebhookSubscriptionInput, WebhookSubscription]:
        return self._create_webhook_subscription

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "calendly.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Calendly connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.calendly.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Calendly client is required")
    return Connection(reference, client.list_scheduled_events(), client.get_scheduled_event(), client.list_event_invitees(), client.cancel_scheduled_event(), client.create_scheduling_link(), client.create_webhook_subscription(), client)

setattr(new_connection, '__module__', 'dex_connectors.calendly.connection')


def new_connection_with_operations(
    list_scheduled_events: Query[ListScheduledEventsInput, ScheduledEventPage],
    get_scheduled_event: Query[GetScheduledEventInput, ScheduledEvent],
    list_event_invitees: Query[ListEventInviteesInput, EventInviteePage],
    cancel_scheduled_event: Mutation[CancelScheduledEventInput, ScheduledEventCancellation],
    create_scheduling_link: Mutation[CreateSchedulingLinkInput, SchedulingLink],
    create_webhook_subscription: Mutation[CreateWebhookSubscriptionInput, WebhookSubscription],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_scheduled_events, get_scheduled_event, list_event_invitees, cancel_scheduled_event, create_scheduling_link, create_webhook_subscription)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.calendly.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Calendly stored credential does not match the connector credential fields")
    if set(material) - set(('access_token', 'webhook_signing_key', 'oauth_client_id', 'oauth_client_secret', 'refresh_token', 'auth_method')):
        raise ValueError("Calendly stored credential does not match the connector credential fields")
    is_shared = shared
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Calendly credential access_token has an invalid type")
    webhook_signing_key = material.get("webhook_signing_key")
    if webhook_signing_key is not None and not (isinstance(webhook_signing_key, str)):
        raise ValueError("Calendly credential webhook_signing_key has an invalid type")
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Calendly credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Calendly credential oauth_client_secret has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Calendly credential refresh_token has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Calendly credential auth_method is invalid")
    method = selected_method or "personal-access-token"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "personal-access-token":
        if not access_token:
            raise ValueError("Calendly credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        values["webhook_signing_key"] = SecretString(webhook_signing_key or '')
    elif method == "calendly-oauth":
        if not oauth_client_id:
            raise ValueError("Calendly credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Calendly credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Calendly credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Calendly credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["webhook_signing_key"] = SecretString(webhook_signing_key or '')
    else:
        raise ValueError("Calendly credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "personal-access-token"
    material['auth_method'] = method
    if method == "personal-access-token":
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.webhook_signing_key.reveal() if credentials.webhook_signing_key is not None else ''):
            material["webhook_signing_key"] = (credentials.webhook_signing_key.reveal() if credentials.webhook_signing_key is not None else '')
    elif method == "calendly-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if (credentials.webhook_signing_key.reveal() if credentials.webhook_signing_key is not None else ''):
            material["webhook_signing_key"] = (credentials.webhook_signing_key.reveal() if credentials.webhook_signing_key is not None else '')
    else:
        raise ValueError("Calendly credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Calendly", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Calendly connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Calendly project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Calendly project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.calendly.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes", "webhookMaxBodyBytes", "webhookSignatureTolerance"}:
        raise ValueError("Calendly configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Calendly configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "webhookMaxBodyBytes" in value:
        webhook_max_body_bytes = value["webhookMaxBodyBytes"]
        if not (type(webhook_max_body_bytes) is int):
            raise ValueError("Calendly configuration webhookMaxBodyBytes has an invalid type")
        config_arguments['webhook_max_body_bytes'] = webhook_max_body_bytes
    if "webhookSignatureTolerance" in value:
        webhook_signature_tolerance = value["webhookSignatureTolerance"]
        if not (type(webhook_signature_tolerance) is int):
            raise ValueError("Calendly configuration webhookSignatureTolerance has an invalid type")
        config_arguments['webhook_signature_tolerance_seconds'] = webhook_signature_tolerance / 1_000_000_000
    return Config(**config_arguments)
