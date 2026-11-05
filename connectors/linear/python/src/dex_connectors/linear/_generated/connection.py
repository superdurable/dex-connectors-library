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
from .definitions import CONNECTOR_ID, ADD_COMMENT_DEFINITION, CREATE_ISSUE_DEFINITION, FIND_USER_BY_EMAIL_DEFINITION, GET_ISSUE_DEFINITION, LIST_WORKFLOW_STATES_DEFINITION, SEARCH_ISSUES_DEFINITION, UPDATE_ISSUE_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import AddCommentInput, AddCommentOutput, CreateIssueInput, CreateIssueOutput, FindUserByEmailInput, GetIssueInput, Issue, ListWorkflowStatesInput, ListWorkflowStatesOutput, SearchIssuesInput, SearchIssuesOutput, UpdateIssueInput, UpdateIssueOutput, User

class Connection:

    __slots__ = ('_search_issues', '_get_issue', '_create_issue', '_update_issue', '_add_comment', '_find_user_by_email', '_list_workflow_states', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_issues: Query[SearchIssuesInput, SearchIssuesOutput],
        get_issue: Query[GetIssueInput, Issue],
        create_issue: Mutation[CreateIssueInput, CreateIssueOutput],
        update_issue: Mutation[UpdateIssueInput, UpdateIssueOutput],
        add_comment: Mutation[AddCommentInput, AddCommentOutput],
        find_user_by_email: Query[FindUserByEmailInput, User],
        list_workflow_states: Query[ListWorkflowStatesInput, ListWorkflowStatesOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or search_issues is None or get_issue is None or create_issue is None or update_issue is None or add_comment is None or find_user_by_email is None or list_workflow_states is None:
            raise ValueError("Linear connection provider and operations are required")
        if search_issues.definition().operation != SEARCH_ISSUES_DEFINITION.operation:
            raise ValueError("Linear connection operation identity differs")
        if get_issue.definition().operation != GET_ISSUE_DEFINITION.operation:
            raise ValueError("Linear connection operation identity differs")
        if create_issue.definition().operation != CREATE_ISSUE_DEFINITION.operation:
            raise ValueError("Linear connection operation identity differs")
        if update_issue.definition().operation != UPDATE_ISSUE_DEFINITION.operation:
            raise ValueError("Linear connection operation identity differs")
        if add_comment.definition().operation != ADD_COMMENT_DEFINITION.operation:
            raise ValueError("Linear connection operation identity differs")
        if find_user_by_email.definition().operation != FIND_USER_BY_EMAIL_DEFINITION.operation:
            raise ValueError("Linear connection operation identity differs")
        if list_workflow_states.definition().operation != LIST_WORKFLOW_STATES_DEFINITION.operation:
            raise ValueError("Linear connection operation identity differs")
        self.reference = reference
        self._search_issues = search_issues
        self._get_issue = get_issue
        self._create_issue = create_issue
        self._update_issue = update_issue
        self._add_comment = add_comment
        self._find_user_by_email = find_user_by_email
        self._list_workflow_states = list_workflow_states
        self._client = client

    def search_issues(self) -> Query[SearchIssuesInput, SearchIssuesOutput]:
        return self._search_issues

    def get_issue(self) -> Query[GetIssueInput, Issue]:
        return self._get_issue

    def create_issue(self) -> Mutation[CreateIssueInput, CreateIssueOutput]:
        return self._create_issue

    def update_issue(self) -> Mutation[UpdateIssueInput, UpdateIssueOutput]:
        return self._update_issue

    def add_comment(self) -> Mutation[AddCommentInput, AddCommentOutput]:
        return self._add_comment

    def find_user_by_email(self) -> Query[FindUserByEmailInput, User]:
        return self._find_user_by_email

    def list_workflow_states(self) -> Query[ListWorkflowStatesInput, ListWorkflowStatesOutput]:
        return self._list_workflow_states

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "linear.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Linear connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.linear.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Linear client is required")
    return Connection(reference, client.search_issues(), client.get_issue(), client.create_issue(), client.update_issue(), client.add_comment(), client.find_user_by_email(), client.list_workflow_states(), client)

setattr(new_connection, '__module__', 'dex_connectors.linear.connection')


def new_connection_with_operations(
    search_issues: Query[SearchIssuesInput, SearchIssuesOutput],
    get_issue: Query[GetIssueInput, Issue],
    create_issue: Mutation[CreateIssueInput, CreateIssueOutput],
    update_issue: Mutation[UpdateIssueInput, UpdateIssueOutput],
    add_comment: Mutation[AddCommentInput, AddCommentOutput],
    find_user_by_email: Query[FindUserByEmailInput, User],
    list_workflow_states: Query[ListWorkflowStatesInput, ListWorkflowStatesOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_issues, get_issue, create_issue, update_issue, add_comment, find_user_by_email, list_workflow_states)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.linear.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Linear stored credential does not match the connector credential fields")
    if set(material) - set(('api_key', 'webhook_signing_secret', 'oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'auth_method')):
        raise ValueError("Linear stored credential does not match the connector credential fields")
    is_shared = shared
    api_key = material.get("api_key")
    if api_key is not None and not (isinstance(api_key, str)):
        raise ValueError("Linear credential api_key has an invalid type")
    webhook_signing_secret = material.get("webhook_signing_secret")
    if webhook_signing_secret is not None and not (isinstance(webhook_signing_secret, str)):
        raise ValueError("Linear credential webhook_signing_secret has an invalid type")
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Linear credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Linear credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Linear credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Linear credential refresh_token has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Linear credential auth_method is invalid")
    method = selected_method or "personal-api-key"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "personal-api-key":
        if not api_key:
            raise ValueError("Linear credential api_key is required")
        if api_key:
            values["api_key"] = SecretString(api_key)
        values["webhook_signing_secret"] = SecretString(webhook_signing_secret or '')
    elif method == "linear-oauth":
        if not oauth_client_id:
            raise ValueError("Linear credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Linear credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Linear credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Linear credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["webhook_signing_secret"] = SecretString(webhook_signing_secret or '')
    else:
        raise ValueError("Linear credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "personal-api-key"
    material['auth_method'] = method
    if method == "personal-api-key":
        if (credentials.api_key.reveal() if credentials.api_key is not None else ''):
            material["api_key"] = (credentials.api_key.reveal() if credentials.api_key is not None else '')
        if (credentials.webhook_signing_secret.reveal() if credentials.webhook_signing_secret is not None else ''):
            material["webhook_signing_secret"] = (credentials.webhook_signing_secret.reveal() if credentials.webhook_signing_secret is not None else '')
    elif method == "linear-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if (credentials.webhook_signing_secret.reveal() if credentials.webhook_signing_secret is not None else ''):
            material["webhook_signing_secret"] = (credentials.webhook_signing_secret.reveal() if credentials.webhook_signing_secret is not None else '')
    else:
        raise ValueError("Linear credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Linear", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Linear connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Linear project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Linear project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.linear.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes", "webhookMaxBodyBytes", "webhookSignatureTolerance"}:
        raise ValueError("Linear configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Linear configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "webhookMaxBodyBytes" in value:
        webhook_max_body_bytes = value["webhookMaxBodyBytes"]
        if not (type(webhook_max_body_bytes) is int):
            raise ValueError("Linear configuration webhookMaxBodyBytes has an invalid type")
        config_arguments['webhook_max_body_bytes'] = webhook_max_body_bytes
    if "webhookSignatureTolerance" in value:
        webhook_signature_tolerance = value["webhookSignatureTolerance"]
        if not (type(webhook_signature_tolerance) is int):
            raise ValueError("Linear configuration webhookSignatureTolerance has an invalid type")
        config_arguments['webhook_signature_tolerance_seconds'] = webhook_signature_tolerance / 1_000_000_000
    return Config(**config_arguments)
