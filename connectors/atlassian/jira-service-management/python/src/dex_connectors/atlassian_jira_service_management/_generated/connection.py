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
from .definitions import CONNECTOR_ID, PROVIDER, ADD_COMMENT_DEFINITION, CREATE_TICKET_DEFINITION, FIND_CUSTOMER_BY_EMAIL_DEFINITION, GET_TICKET_DEFINITION, SEARCH_TICKETS_DEFINITION, TRANSITION_TICKET_DEFINITION, UPDATE_TICKET_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import AddCommentInput, AddCommentOutput, CreateTicketInput, CreateTicketOutput, CustomerMatches, FindCustomerByEmailInput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, TicketDetails, TransitionTicketInput, TransitionTicketOutput, UpdateTicketInput, UpdateTicketOutput

class Connection:

    __slots__ = ('_find_customer_by_email', '_search_tickets', '_get_ticket', '_create_ticket', '_update_ticket', '_transition_ticket', '_add_comment', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        find_customer_by_email: Query[FindCustomerByEmailInput, CustomerMatches],
        search_tickets: Query[SearchTicketsInput, SearchTicketsOutput],
        get_ticket: Query[GetTicketInput, TicketDetails],
        create_ticket: Mutation[CreateTicketInput, CreateTicketOutput],
        update_ticket: Mutation[UpdateTicketInput, UpdateTicketOutput],
        transition_ticket: Mutation[TransitionTicketInput, TransitionTicketOutput],
        add_comment: Mutation[AddCommentInput, AddCommentOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or find_customer_by_email is None or search_tickets is None or get_ticket is None or create_ticket is None or update_ticket is None or transition_ticket is None or add_comment is None:
            raise ValueError("Jira Service Management connection provider and operations are required")
        if find_customer_by_email.definition().operation != FIND_CUSTOMER_BY_EMAIL_DEFINITION.operation:
            raise ValueError("Jira Service Management connection operation identity differs")
        if search_tickets.definition().operation != SEARCH_TICKETS_DEFINITION.operation:
            raise ValueError("Jira Service Management connection operation identity differs")
        if get_ticket.definition().operation != GET_TICKET_DEFINITION.operation:
            raise ValueError("Jira Service Management connection operation identity differs")
        if create_ticket.definition().operation != CREATE_TICKET_DEFINITION.operation:
            raise ValueError("Jira Service Management connection operation identity differs")
        if update_ticket.definition().operation != UPDATE_TICKET_DEFINITION.operation:
            raise ValueError("Jira Service Management connection operation identity differs")
        if transition_ticket.definition().operation != TRANSITION_TICKET_DEFINITION.operation:
            raise ValueError("Jira Service Management connection operation identity differs")
        if add_comment.definition().operation != ADD_COMMENT_DEFINITION.operation:
            raise ValueError("Jira Service Management connection operation identity differs")
        self.reference = reference
        self._find_customer_by_email = find_customer_by_email
        self._search_tickets = search_tickets
        self._get_ticket = get_ticket
        self._create_ticket = create_ticket
        self._update_ticket = update_ticket
        self._transition_ticket = transition_ticket
        self._add_comment = add_comment
        self._client = client

    def find_customer_by_email(self) -> Query[FindCustomerByEmailInput, CustomerMatches]:
        return self._find_customer_by_email

    def search_tickets(self) -> Query[SearchTicketsInput, SearchTicketsOutput]:
        return self._search_tickets

    def get_ticket(self) -> Query[GetTicketInput, TicketDetails]:
        return self._get_ticket

    def create_ticket(self) -> Mutation[CreateTicketInput, CreateTicketOutput]:
        return self._create_ticket

    def update_ticket(self) -> Mutation[UpdateTicketInput, UpdateTicketOutput]:
        return self._update_ticket

    def transition_ticket(self) -> Mutation[TransitionTicketInput, TransitionTicketOutput]:
        return self._transition_ticket

    def add_comment(self) -> Mutation[AddCommentInput, AddCommentOutput]:
        return self._add_comment

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "atlassian_jira_service_management.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Jira Service Management connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.atlassian_jira_service_management.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Jira Service Management client is required")
    return Connection(reference, client.find_customer_by_email(), client.search_tickets(), client.get_ticket(), client.create_ticket(), client.update_ticket(), client.transition_ticket(), client.add_comment(), client)

setattr(new_connection, '__module__', 'dex_connectors.atlassian_jira_service_management.connection')


def new_connection_with_operations(
    find_customer_by_email: Query[FindCustomerByEmailInput, CustomerMatches],
    search_tickets: Query[SearchTicketsInput, SearchTicketsOutput],
    get_ticket: Query[GetTicketInput, TicketDetails],
    create_ticket: Mutation[CreateTicketInput, CreateTicketOutput],
    update_ticket: Mutation[UpdateTicketInput, UpdateTicketOutput],
    transition_ticket: Mutation[TransitionTicketInput, TransitionTicketOutput],
    add_comment: Mutation[AddCommentInput, AddCommentOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, find_customer_by_email, search_tickets, get_ticket, create_ticket, update_ticket, transition_ticket, add_comment)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.atlassian_jira_service_management.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Jira Service Management stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token')):
        raise ValueError("Jira Service Management stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Jira Service Management credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Jira Service Management credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Jira Service Management credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Jira Service Management credential refresh_token has an invalid type")
    if not oauth_client_id:
        raise ValueError("Jira Service Management credential oauth_client_id is required")
    if not oauth_client_secret and not is_shared:
        raise ValueError("Jira Service Management credential oauth_client_secret is required")
    if not access_token:
        raise ValueError("Jira Service Management credential access_token is required")
    if not refresh_token and not is_shared:
        raise ValueError("Jira Service Management credential refresh_token is required")
    return Credentials(oauth_client_id, SecretString(oauth_client_secret or ''), SecretString(access_token), SecretString(refresh_token or ''))


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    if credentials.oauth_client_id:
        material["oauth_client_id"] = credentials.oauth_client_id
    if credentials.oauth_client_secret.reveal():
        material["oauth_client_secret"] = credentials.oauth_client_secret.reveal()
    if credentials.access_token.reveal():
        material["access_token"] = credentials.access_token.reveal()
    if credentials.refresh_token.reveal():
        material["refresh_token"] = credentials.refresh_token.reveal()
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Jira Service Management", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Jira Service Management connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Jira Service Management project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Jira Service Management project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.atlassian_jira_service_management.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"cloudId", "endpoint", "maxResponseBytes"}:
        raise ValueError("Jira Service Management configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "cloudId" in value:
        cloud_id = value["cloudId"]
        if not (isinstance(cloud_id, str)):
            raise ValueError("Jira Service Management configuration cloudId has an invalid type")
        config_arguments['cloud_id'] = cloud_id
    else:
        config_arguments['cloud_id'] = ''
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Jira Service Management configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Jira Service Management configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
