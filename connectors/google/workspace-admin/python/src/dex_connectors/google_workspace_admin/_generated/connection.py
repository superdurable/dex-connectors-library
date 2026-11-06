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
from .definitions import CONNECTOR_ID, PROVIDER, ADD_USER_TO_GROUP_DEFINITION, CREATE_USER_DEFINITION, GET_USER_DEFINITION, LIST_USERS_DEFINITION, REMOVE_USER_FROM_GROUP_DEFINITION, SUSPEND_USER_DEFINITION, UNSUSPEND_USER_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import AddUserToGroupInput, AddUserToGroupOutput, CreateUserInput, CreateUserOutput, GetUserInput, ListUsersInput, ListUsersOutput, RemoveUserFromGroupInput, RemoveUserFromGroupOutput, SuspendUserInput, UnsuspendUserInput, User

class Connection:

    __slots__ = ('_get_user', '_list_users', '_create_user', '_suspend_user', '_unsuspend_user', '_add_user_to_group', '_remove_user_from_group', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_user: Query[GetUserInput, User],
        list_users: Query[ListUsersInput, ListUsersOutput],
        create_user: Mutation[CreateUserInput, CreateUserOutput],
        suspend_user: Mutation[SuspendUserInput, User],
        unsuspend_user: Mutation[UnsuspendUserInput, User],
        add_user_to_group: Mutation[AddUserToGroupInput, AddUserToGroupOutput],
        remove_user_from_group: Mutation[RemoveUserFromGroupInput, RemoveUserFromGroupOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or get_user is None or list_users is None or create_user is None or suspend_user is None or unsuspend_user is None or add_user_to_group is None or remove_user_from_group is None:
            raise ValueError("Google Workspace Admin connection provider and operations are required")
        if get_user.definition().operation != GET_USER_DEFINITION.operation:
            raise ValueError("Google Workspace Admin connection operation identity differs")
        if list_users.definition().operation != LIST_USERS_DEFINITION.operation:
            raise ValueError("Google Workspace Admin connection operation identity differs")
        if create_user.definition().operation != CREATE_USER_DEFINITION.operation:
            raise ValueError("Google Workspace Admin connection operation identity differs")
        if suspend_user.definition().operation != SUSPEND_USER_DEFINITION.operation:
            raise ValueError("Google Workspace Admin connection operation identity differs")
        if unsuspend_user.definition().operation != UNSUSPEND_USER_DEFINITION.operation:
            raise ValueError("Google Workspace Admin connection operation identity differs")
        if add_user_to_group.definition().operation != ADD_USER_TO_GROUP_DEFINITION.operation:
            raise ValueError("Google Workspace Admin connection operation identity differs")
        if remove_user_from_group.definition().operation != REMOVE_USER_FROM_GROUP_DEFINITION.operation:
            raise ValueError("Google Workspace Admin connection operation identity differs")
        self.reference = reference
        self._get_user = get_user
        self._list_users = list_users
        self._create_user = create_user
        self._suspend_user = suspend_user
        self._unsuspend_user = unsuspend_user
        self._add_user_to_group = add_user_to_group
        self._remove_user_from_group = remove_user_from_group
        self._client = client

    def get_user(self) -> Query[GetUserInput, User]:
        return self._get_user

    def list_users(self) -> Query[ListUsersInput, ListUsersOutput]:
        return self._list_users

    def create_user(self) -> Mutation[CreateUserInput, CreateUserOutput]:
        return self._create_user

    def suspend_user(self) -> Mutation[SuspendUserInput, User]:
        return self._suspend_user

    def unsuspend_user(self) -> Mutation[UnsuspendUserInput, User]:
        return self._unsuspend_user

    def add_user_to_group(self) -> Mutation[AddUserToGroupInput, AddUserToGroupOutput]:
        return self._add_user_to_group

    def remove_user_from_group(self) -> Mutation[RemoveUserFromGroupInput, RemoveUserFromGroupOutput]:
        return self._remove_user_from_group

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "google_workspace_admin.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Google Workspace Admin connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.google_workspace_admin.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Google Workspace Admin client is required")
    return Connection(reference, client.get_user(), client.list_users(), client.create_user(), client.suspend_user(), client.unsuspend_user(), client.add_user_to_group(), client.remove_user_from_group(), client)

setattr(new_connection, '__module__', 'dex_connectors.google_workspace_admin.connection')


def new_connection_with_operations(
    get_user: Query[GetUserInput, User],
    list_users: Query[ListUsersInput, ListUsersOutput],
    create_user: Mutation[CreateUserInput, CreateUserOutput],
    suspend_user: Mutation[SuspendUserInput, User],
    unsuspend_user: Mutation[UnsuspendUserInput, User],
    add_user_to_group: Mutation[AddUserToGroupInput, AddUserToGroupOutput],
    remove_user_from_group: Mutation[RemoveUserFromGroupInput, RemoveUserFromGroupOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_user, list_users, create_user, suspend_user, unsuspend_user, add_user_to_group, remove_user_from_group)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.google_workspace_admin.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Google Workspace Admin stored credential does not match the connector credential fields")
    if set(material) - set(('service_account_key', 'delegated_user', 'access_token', 'oauth_client_id', 'oauth_client_secret', 'refresh_token', 'auth_method')):
        raise ValueError("Google Workspace Admin stored credential does not match the connector credential fields")
    is_shared = shared
    service_account_key = material.get("service_account_key")
    if service_account_key is not None and not (isinstance(service_account_key, str)):
        raise ValueError("Google Workspace Admin credential service_account_key has an invalid type")
    delegated_user = material.get("delegated_user")
    if delegated_user is not None and not (isinstance(delegated_user, str)):
        raise ValueError("Google Workspace Admin credential delegated_user has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Google Workspace Admin credential access_token has an invalid type")
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Google Workspace Admin credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Google Workspace Admin credential oauth_client_secret has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Google Workspace Admin credential refresh_token has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Google Workspace Admin credential auth_method is invalid")
    method = selected_method or "workspace-domain-delegation"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "workspace-domain-delegation":
        if not service_account_key and not is_shared:
            raise ValueError("Google Workspace Admin credential service_account_key is required")
        if service_account_key:
            values["service_account_key"] = SecretString(service_account_key)
        if not delegated_user:
            raise ValueError("Google Workspace Admin credential delegated_user is required")
        if delegated_user:
            values["delegated_user"] = delegated_user
        if not access_token and is_shared:
            raise ValueError("Google Workspace Admin credential access_token is required")
        values["access_token"] = SecretString(access_token or '')
    elif method == "google-oauth":
        if not oauth_client_id:
            raise ValueError("Google Workspace Admin credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Google Workspace Admin credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Google Workspace Admin credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Google Workspace Admin credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    else:
        raise ValueError("Google Workspace Admin credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "workspace-domain-delegation"
    material['auth_method'] = method
    if method == "workspace-domain-delegation":
        if (credentials.service_account_key.reveal() if credentials.service_account_key is not None else ''):
            material["service_account_key"] = (credentials.service_account_key.reveal() if credentials.service_account_key is not None else '')
        if credentials.delegated_user:
            material["delegated_user"] = credentials.delegated_user
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
    elif method == "google-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
    else:
        raise ValueError("Google Workspace Admin credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Google Workspace Admin", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Google Workspace Admin connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Google Workspace Admin project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Google Workspace Admin project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.google_workspace_admin.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "maxResponseBytes", "listUsersPageSize"}:
        raise ValueError("Google Workspace Admin configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Google Workspace Admin configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Google Workspace Admin configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "listUsersPageSize" in value:
        list_users_page_size = value["listUsersPageSize"]
        if not (type(list_users_page_size) is int):
            raise ValueError("Google Workspace Admin configuration listUsersPageSize has an invalid type")
        config_arguments['list_users_page_size'] = list_users_page_size
    return Config(**config_arguments)
