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
from .definitions import CONNECTOR_ID, PROVIDER, ADD_COMMENT_DEFINITION, CREATE_ISSUE_DEFINITION, GET_ISSUE_DEFINITION, SEARCH_ISSUES_DEFINITION, TRANSITION_ISSUE_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import AddCommentInput, AddCommentOutput, CreateIssueInput, CreateIssueOutput, GetIssueInput, Issue, SearchIssuesInput, SearchIssuesOutput, TransitionIssueInput, TransitionIssueOutput

class Connection:

    __slots__ = ('_search_issues', '_get_issue', '_create_issue', '_transition_issue', '_add_comment', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_issues: Query[SearchIssuesInput, SearchIssuesOutput],
        get_issue: Query[GetIssueInput, Issue],
        create_issue: Mutation[CreateIssueInput, CreateIssueOutput],
        transition_issue: Mutation[TransitionIssueInput, TransitionIssueOutput],
        add_comment: Mutation[AddCommentInput, AddCommentOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or search_issues is None or get_issue is None or create_issue is None or transition_issue is None or add_comment is None:
            raise ValueError("Jira connection provider and operations are required")
        if search_issues.definition().operation != SEARCH_ISSUES_DEFINITION.operation:
            raise ValueError("Jira connection operation identity differs")
        if get_issue.definition().operation != GET_ISSUE_DEFINITION.operation:
            raise ValueError("Jira connection operation identity differs")
        if create_issue.definition().operation != CREATE_ISSUE_DEFINITION.operation:
            raise ValueError("Jira connection operation identity differs")
        if transition_issue.definition().operation != TRANSITION_ISSUE_DEFINITION.operation:
            raise ValueError("Jira connection operation identity differs")
        if add_comment.definition().operation != ADD_COMMENT_DEFINITION.operation:
            raise ValueError("Jira connection operation identity differs")
        self.reference = reference
        self._search_issues = search_issues
        self._get_issue = get_issue
        self._create_issue = create_issue
        self._transition_issue = transition_issue
        self._add_comment = add_comment
        self._client = client

    def search_issues(self) -> Query[SearchIssuesInput, SearchIssuesOutput]:
        return self._search_issues

    def get_issue(self) -> Query[GetIssueInput, Issue]:
        return self._get_issue

    def create_issue(self) -> Mutation[CreateIssueInput, CreateIssueOutput]:
        return self._create_issue

    def transition_issue(self) -> Mutation[TransitionIssueInput, TransitionIssueOutput]:
        return self._transition_issue

    def add_comment(self) -> Mutation[AddCommentInput, AddCommentOutput]:
        return self._add_comment

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "atlassian_jira.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Jira connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.atlassian_jira.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Jira client is required")
    return Connection(reference, client.search_issues(), client.get_issue(), client.create_issue(), client.transition_issue(), client.add_comment(), client)

setattr(new_connection, '__module__', 'dex_connectors.atlassian_jira.connection')


def new_connection_with_operations(
    search_issues: Query[SearchIssuesInput, SearchIssuesOutput],
    get_issue: Query[GetIssueInput, Issue],
    create_issue: Mutation[CreateIssueInput, CreateIssueOutput],
    transition_issue: Mutation[TransitionIssueInput, TransitionIssueOutput],
    add_comment: Mutation[AddCommentInput, AddCommentOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_issues, get_issue, create_issue, transition_issue, add_comment)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.atlassian_jira.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Jira stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token')):
        raise ValueError("Jira stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Jira credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Jira credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Jira credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Jira credential refresh_token has an invalid type")
    if not oauth_client_id:
        raise ValueError("Jira credential oauth_client_id is required")
    if not oauth_client_secret and not is_shared:
        raise ValueError("Jira credential oauth_client_secret is required")
    if not access_token:
        raise ValueError("Jira credential access_token is required")
    if not refresh_token and not is_shared:
        raise ValueError("Jira credential refresh_token is required")
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
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Jira", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Jira connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Jira project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Jira project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.atlassian_jira.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"cloudId", "endpoint", "maxResponseBytes"}:
        raise ValueError("Jira configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "cloudId" in value:
        cloud_id = value["cloudId"]
        if not (isinstance(cloud_id, str)):
            raise ValueError("Jira configuration cloudId has an invalid type")
        config_arguments['cloud_id'] = cloud_id
    else:
        config_arguments['cloud_id'] = ''
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Jira configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Jira configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
