# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString, CredentialRefreshDriver
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_AUTHENTICATED_PROFILE_DEFINITION, LIST_COMMITS_DEFINITION, LIST_MERGED_PULL_REQUESTS_DEFINITION, LIST_PUBLIC_REPOSITORIES_DEFINITION, LIST_PULL_REQUEST_FILES_DEFINITION, LIST_RELEASES_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import AuthenticatedProfile, CommitPage, GetAuthenticatedProfileInput, ListCommitsInput, ListMergedPullRequestsInput, ListPublicRepositoriesInput, ListPullRequestFilesInput, ListReleasesInput, MergedPullRequestPage, PublicRepositories, PullRequestFilePage, ReleasePage

class Connection:

    __slots__ = ('_get_authenticated_profile', '_list_public_repositories', '_list_merged_pull_requests', '_list_pull_request_files', '_list_releases', '_list_commits', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_authenticated_profile: Query[GetAuthenticatedProfileInput, AuthenticatedProfile],
        list_public_repositories: Query[ListPublicRepositoriesInput, PublicRepositories],
        list_merged_pull_requests: Query[ListMergedPullRequestsInput, MergedPullRequestPage],
        list_pull_request_files: Query[ListPullRequestFilesInput, PullRequestFilePage],
        list_releases: Query[ListReleasesInput, ReleasePage],
        list_commits: Query[ListCommitsInput, CommitPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_authenticated_profile is None or list_public_repositories is None or list_merged_pull_requests is None or list_pull_request_files is None or list_releases is None or list_commits is None:
            raise ValueError("GitHub connection provider and operations are required")
        if get_authenticated_profile.definition().operation != GET_AUTHENTICATED_PROFILE_DEFINITION.operation:
            raise ValueError("GitHub connection operation identity differs")
        if list_public_repositories.definition().operation != LIST_PUBLIC_REPOSITORIES_DEFINITION.operation:
            raise ValueError("GitHub connection operation identity differs")
        if list_merged_pull_requests.definition().operation != LIST_MERGED_PULL_REQUESTS_DEFINITION.operation:
            raise ValueError("GitHub connection operation identity differs")
        if list_pull_request_files.definition().operation != LIST_PULL_REQUEST_FILES_DEFINITION.operation:
            raise ValueError("GitHub connection operation identity differs")
        if list_releases.definition().operation != LIST_RELEASES_DEFINITION.operation:
            raise ValueError("GitHub connection operation identity differs")
        if list_commits.definition().operation != LIST_COMMITS_DEFINITION.operation:
            raise ValueError("GitHub connection operation identity differs")
        self.reference = reference
        self._get_authenticated_profile = get_authenticated_profile
        self._list_public_repositories = list_public_repositories
        self._list_merged_pull_requests = list_merged_pull_requests
        self._list_pull_request_files = list_pull_request_files
        self._list_releases = list_releases
        self._list_commits = list_commits
        self._client = client

    def get_authenticated_profile(self) -> Query[GetAuthenticatedProfileInput, AuthenticatedProfile]:
        return self._get_authenticated_profile

    def list_public_repositories(self) -> Query[ListPublicRepositoriesInput, PublicRepositories]:
        return self._list_public_repositories

    def list_merged_pull_requests(self) -> Query[ListMergedPullRequestsInput, MergedPullRequestPage]:
        return self._list_merged_pull_requests

    def list_pull_request_files(self) -> Query[ListPullRequestFilesInput, PullRequestFilePage]:
        return self._list_pull_request_files

    def list_releases(self) -> Query[ListReleasesInput, ReleasePage]:
        return self._list_releases

    def list_commits(self) -> Query[ListCommitsInput, CommitPage]:
        return self._list_commits

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "github.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("GitHub connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.github.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("GitHub client is required")
    return Connection(reference, client.get_authenticated_profile(), client.list_public_repositories(), client.list_merged_pull_requests(), client.list_pull_request_files(), client.list_releases(), client.list_commits(), client)

setattr(new_connection, '__module__', 'dex_connectors.github.connection')


def new_connection_with_operations(
    get_authenticated_profile: Query[GetAuthenticatedProfileInput, AuthenticatedProfile],
    list_public_repositories: Query[ListPublicRepositoriesInput, PublicRepositories],
    list_merged_pull_requests: Query[ListMergedPullRequestsInput, MergedPullRequestPage],
    list_pull_request_files: Query[ListPullRequestFilesInput, PullRequestFilePage],
    list_releases: Query[ListReleasesInput, ReleasePage],
    list_commits: Query[ListCommitsInput, CommitPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_authenticated_profile, list_public_repositories, list_merged_pull_requests, list_pull_request_files, list_releases, list_commits)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.github.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("GitHub stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token')):
        raise ValueError("GitHub stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("GitHub credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("GitHub credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("GitHub credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("GitHub credential refresh_token has an invalid type")
    if not oauth_client_id:
        raise ValueError("GitHub credential oauth_client_id is required")
    if not oauth_client_secret and not is_shared:
        raise ValueError("GitHub credential oauth_client_secret is required")
    if not access_token:
        raise ValueError("GitHub credential access_token is required")
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
        self.driver = ManifestCredentialRefreshDriver[Credentials]("GitHub", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("GitHub connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("GitHub project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("GitHub project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.github.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "apiVersion", "timeout", "maxResponseBytes", "defaultRepositoryLimit", "maxRepositories", "maxPullRequestBodyCharacters", "maxPatchCharacters", "maxCommitMessageCharacters"}:
        raise ValueError("GitHub configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("GitHub configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "apiVersion" in value:
        api_version = value["apiVersion"]
        if not (isinstance(api_version, str)):
            raise ValueError("GitHub configuration apiVersion has an invalid type")
        config_arguments['api_version'] = api_version
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("GitHub configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("GitHub configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "defaultRepositoryLimit" in value:
        default_repository_limit = value["defaultRepositoryLimit"]
        if not (type(default_repository_limit) is int):
            raise ValueError("GitHub configuration defaultRepositoryLimit has an invalid type")
        config_arguments['default_repository_limit'] = default_repository_limit
    if "maxRepositories" in value:
        max_repositories = value["maxRepositories"]
        if not (type(max_repositories) is int):
            raise ValueError("GitHub configuration maxRepositories has an invalid type")
        config_arguments['max_repositories'] = max_repositories
    if "maxPullRequestBodyCharacters" in value:
        max_pull_request_body_characters = value["maxPullRequestBodyCharacters"]
        if not (type(max_pull_request_body_characters) is int):
            raise ValueError("GitHub configuration maxPullRequestBodyCharacters has an invalid type")
        config_arguments['max_pull_request_body_characters'] = max_pull_request_body_characters
    if "maxPatchCharacters" in value:
        max_patch_characters = value["maxPatchCharacters"]
        if not (type(max_patch_characters) is int):
            raise ValueError("GitHub configuration maxPatchCharacters has an invalid type")
        config_arguments['max_patch_characters'] = max_patch_characters
    if "maxCommitMessageCharacters" in value:
        max_commit_message_characters = value["maxCommitMessageCharacters"]
        if not (type(max_commit_message_characters) is int):
            raise ValueError("GitHub configuration maxCommitMessageCharacters has an invalid type")
        config_arguments['max_commit_message_characters'] = max_commit_message_characters
    return Config(**config_arguments)
