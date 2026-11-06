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
from .definitions import CONNECTOR_ID, PROVIDER, ADD_USER_TO_GROUP_DEFINITION, CREATE_USER_DEFINITION, DISABLE_USER_DEFINITION, ENABLE_USER_DEFINITION, GET_USER_DEFINITION, LIST_USERS_DEFINITION, REMOVE_USER_FROM_GROUP_DEFINITION, REVOKE_SIGN_IN_SESSIONS_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import AddUserToGroupInput, AddUserToGroupOutput, CreateUserInput, CreateUserOutput, DisableUserInput, EnableUserInput, GetUserInput, ListUsersInput, ListUsersOutput, RemoveUserFromGroupInput, RemoveUserFromGroupOutput, RevokeSignInSessionsInput, RevokeSignInSessionsOutput, User

class Connection:

    __slots__ = ('_get_user', '_list_users', '_create_user', '_disable_user', '_enable_user', '_revoke_sign_in_sessions', '_add_user_to_group', '_remove_user_from_group', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_user: Query[GetUserInput, User],
        list_users: Query[ListUsersInput, ListUsersOutput],
        create_user: Mutation[CreateUserInput, CreateUserOutput],
        disable_user: Mutation[DisableUserInput, User],
        enable_user: Mutation[EnableUserInput, User],
        revoke_sign_in_sessions: Mutation[RevokeSignInSessionsInput, RevokeSignInSessionsOutput],
        add_user_to_group: Mutation[AddUserToGroupInput, AddUserToGroupOutput],
        remove_user_from_group: Mutation[RemoveUserFromGroupInput, RemoveUserFromGroupOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or get_user is None or list_users is None or create_user is None or disable_user is None or enable_user is None or revoke_sign_in_sessions is None or add_user_to_group is None or remove_user_from_group is None:
            raise ValueError("Microsoft Entra ID connection provider and operations are required")
        if get_user.definition().operation != GET_USER_DEFINITION.operation:
            raise ValueError("Microsoft Entra ID connection operation identity differs")
        if list_users.definition().operation != LIST_USERS_DEFINITION.operation:
            raise ValueError("Microsoft Entra ID connection operation identity differs")
        if create_user.definition().operation != CREATE_USER_DEFINITION.operation:
            raise ValueError("Microsoft Entra ID connection operation identity differs")
        if disable_user.definition().operation != DISABLE_USER_DEFINITION.operation:
            raise ValueError("Microsoft Entra ID connection operation identity differs")
        if enable_user.definition().operation != ENABLE_USER_DEFINITION.operation:
            raise ValueError("Microsoft Entra ID connection operation identity differs")
        if revoke_sign_in_sessions.definition().operation != REVOKE_SIGN_IN_SESSIONS_DEFINITION.operation:
            raise ValueError("Microsoft Entra ID connection operation identity differs")
        if add_user_to_group.definition().operation != ADD_USER_TO_GROUP_DEFINITION.operation:
            raise ValueError("Microsoft Entra ID connection operation identity differs")
        if remove_user_from_group.definition().operation != REMOVE_USER_FROM_GROUP_DEFINITION.operation:
            raise ValueError("Microsoft Entra ID connection operation identity differs")
        self.reference = reference
        self._get_user = get_user
        self._list_users = list_users
        self._create_user = create_user
        self._disable_user = disable_user
        self._enable_user = enable_user
        self._revoke_sign_in_sessions = revoke_sign_in_sessions
        self._add_user_to_group = add_user_to_group
        self._remove_user_from_group = remove_user_from_group
        self._client = client

    def get_user(self) -> Query[GetUserInput, User]:
        return self._get_user

    def list_users(self) -> Query[ListUsersInput, ListUsersOutput]:
        return self._list_users

    def create_user(self) -> Mutation[CreateUserInput, CreateUserOutput]:
        return self._create_user

    def disable_user(self) -> Mutation[DisableUserInput, User]:
        return self._disable_user

    def enable_user(self) -> Mutation[EnableUserInput, User]:
        return self._enable_user

    def revoke_sign_in_sessions(self) -> Mutation[RevokeSignInSessionsInput, RevokeSignInSessionsOutput]:
        return self._revoke_sign_in_sessions

    def add_user_to_group(self) -> Mutation[AddUserToGroupInput, AddUserToGroupOutput]:
        return self._add_user_to_group

    def remove_user_from_group(self) -> Mutation[RemoveUserFromGroupInput, RemoveUserFromGroupOutput]:
        return self._remove_user_from_group

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "microsoft_entra_id.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Microsoft Entra ID connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.microsoft_entra_id.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Microsoft Entra ID client is required")
    return Connection(reference, client.get_user(), client.list_users(), client.create_user(), client.disable_user(), client.enable_user(), client.revoke_sign_in_sessions(), client.add_user_to_group(), client.remove_user_from_group(), client)

setattr(new_connection, '__module__', 'dex_connectors.microsoft_entra_id.connection')


def new_connection_with_operations(
    get_user: Query[GetUserInput, User],
    list_users: Query[ListUsersInput, ListUsersOutput],
    create_user: Mutation[CreateUserInput, CreateUserOutput],
    disable_user: Mutation[DisableUserInput, User],
    enable_user: Mutation[EnableUserInput, User],
    revoke_sign_in_sessions: Mutation[RevokeSignInSessionsInput, RevokeSignInSessionsOutput],
    add_user_to_group: Mutation[AddUserToGroupInput, AddUserToGroupOutput],
    remove_user_from_group: Mutation[RemoveUserFromGroupInput, RemoveUserFromGroupOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_user, list_users, create_user, disable_user, enable_user, revoke_sign_in_sessions, add_user_to_group, remove_user_from_group)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.microsoft_entra_id.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Microsoft Entra ID stored credential does not match the connector credential fields")
    if set(material) - set(('tenant_id', 'client_id', 'client_secret', 'access_token', 'refresh_token', 'auth_method')):
        raise ValueError("Microsoft Entra ID stored credential does not match the connector credential fields")
    is_shared = shared
    tenant_id = material.get("tenant_id")
    if tenant_id is not None and not (isinstance(tenant_id, str)):
        raise ValueError("Microsoft Entra ID credential tenant_id has an invalid type")
    client_id = material.get("client_id")
    if client_id is not None and not (isinstance(client_id, str)):
        raise ValueError("Microsoft Entra ID credential client_id has an invalid type")
    client_secret = material.get("client_secret")
    if client_secret is not None and not (isinstance(client_secret, str)):
        raise ValueError("Microsoft Entra ID credential client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Microsoft Entra ID credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Microsoft Entra ID credential refresh_token has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Microsoft Entra ID credential auth_method is invalid")
    method = selected_method or "entra-app-only"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "entra-app-only":
        if not tenant_id:
            raise ValueError("Microsoft Entra ID credential tenant_id is required")
        if tenant_id:
            values["tenant_id"] = tenant_id
        if not client_id:
            raise ValueError("Microsoft Entra ID credential client_id is required")
        if client_id:
            values["client_id"] = client_id
        if not client_secret and not is_shared:
            raise ValueError("Microsoft Entra ID credential client_secret is required")
        if client_secret:
            values["client_secret"] = SecretString(client_secret)
        if not access_token and is_shared:
            raise ValueError("Microsoft Entra ID credential access_token is required")
        values["access_token"] = SecretString(access_token or '')
    elif method == "microsoft-oauth":
        if not client_id:
            raise ValueError("Microsoft Entra ID credential client_id is required")
        if client_id:
            values["client_id"] = client_id
        if not client_secret and not is_shared:
            raise ValueError("Microsoft Entra ID credential client_secret is required")
        if client_secret:
            values["client_secret"] = SecretString(client_secret)
        if not access_token:
            raise ValueError("Microsoft Entra ID credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Microsoft Entra ID credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    else:
        raise ValueError("Microsoft Entra ID credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "entra-app-only"
    material['auth_method'] = method
    if method == "entra-app-only":
        if credentials.tenant_id:
            material["tenant_id"] = credentials.tenant_id
        if credentials.client_id:
            material["client_id"] = credentials.client_id
        if (credentials.client_secret.reveal() if credentials.client_secret is not None else ''):
            material["client_secret"] = (credentials.client_secret.reveal() if credentials.client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
    elif method == "microsoft-oauth":
        if credentials.client_id:
            material["client_id"] = credentials.client_id
        if (credentials.client_secret.reveal() if credentials.client_secret is not None else ''):
            material["client_secret"] = (credentials.client_secret.reveal() if credentials.client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
    else:
        raise ValueError("Microsoft Entra ID credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Microsoft Entra ID", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Microsoft Entra ID connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Microsoft Entra ID project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Microsoft Entra ID project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.microsoft_entra_id.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes", "listUsersPageSize", "creationKeyAttribute"}:
        raise ValueError("Microsoft Entra ID configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Microsoft Entra ID configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "listUsersPageSize" in value:
        list_users_page_size = value["listUsersPageSize"]
        if not (type(list_users_page_size) is int):
            raise ValueError("Microsoft Entra ID configuration listUsersPageSize has an invalid type")
        config_arguments['list_users_page_size'] = list_users_page_size
    if "creationKeyAttribute" in value:
        creation_key_attribute = value["creationKeyAttribute"]
        if not (isinstance(creation_key_attribute, str) and creation_key_attribute in ('extensionAttribute1', 'extensionAttribute2', 'extensionAttribute3', 'extensionAttribute4', 'extensionAttribute5', 'extensionAttribute6', 'extensionAttribute7', 'extensionAttribute8', 'extensionAttribute9', 'extensionAttribute10', 'extensionAttribute11', 'extensionAttribute12', 'extensionAttribute13', 'extensionAttribute14', 'extensionAttribute15')):
            raise ValueError("Microsoft Entra ID configuration creationKeyAttribute has an invalid type")
        config_arguments['creation_key_attribute'] = creation_key_attribute
    return Config(**config_arguments)
