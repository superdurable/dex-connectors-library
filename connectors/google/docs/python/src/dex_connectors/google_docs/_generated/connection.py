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
from .definitions import CONNECTOR_ID, PROVIDER, APPEND_TEXT_DEFINITION, CREATE_DOCUMENT_DEFINITION, GET_DOCUMENT_TEXT_DEFINITION, REPLACE_DOCUMENT_TEXT_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import AppendTextInput, AppendTextOutput, CreateDocumentInput, CreateDocumentOutput, GetDocumentTextInput, GetDocumentTextOutput, ReplaceDocumentTextInput, ReplaceDocumentTextOutput

class Connection:

    __slots__ = ('_get_document_text', '_create_document', '_replace_document_text', '_append_text', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_document_text: Query[GetDocumentTextInput, GetDocumentTextOutput],
        create_document: Mutation[CreateDocumentInput, CreateDocumentOutput],
        replace_document_text: Mutation[ReplaceDocumentTextInput, ReplaceDocumentTextOutput],
        append_text: Mutation[AppendTextInput, AppendTextOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or get_document_text is None or create_document is None or replace_document_text is None or append_text is None:
            raise ValueError("Google Docs connection provider and operations are required")
        if get_document_text.definition().operation != GET_DOCUMENT_TEXT_DEFINITION.operation:
            raise ValueError("Google Docs connection operation identity differs")
        if create_document.definition().operation != CREATE_DOCUMENT_DEFINITION.operation:
            raise ValueError("Google Docs connection operation identity differs")
        if replace_document_text.definition().operation != REPLACE_DOCUMENT_TEXT_DEFINITION.operation:
            raise ValueError("Google Docs connection operation identity differs")
        if append_text.definition().operation != APPEND_TEXT_DEFINITION.operation:
            raise ValueError("Google Docs connection operation identity differs")
        self.reference = reference
        self._get_document_text = get_document_text
        self._create_document = create_document
        self._replace_document_text = replace_document_text
        self._append_text = append_text
        self._client = client

    def get_document_text(self) -> Query[GetDocumentTextInput, GetDocumentTextOutput]:
        return self._get_document_text

    def create_document(self) -> Mutation[CreateDocumentInput, CreateDocumentOutput]:
        return self._create_document

    def replace_document_text(self) -> Mutation[ReplaceDocumentTextInput, ReplaceDocumentTextOutput]:
        return self._replace_document_text

    def append_text(self) -> Mutation[AppendTextInput, AppendTextOutput]:
        return self._append_text

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "google_docs.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Google Docs connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.google_docs.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Google Docs client is required")
    return Connection(reference, client.get_document_text(), client.create_document(), client.replace_document_text(), client.append_text(), client)

setattr(new_connection, '__module__', 'dex_connectors.google_docs.connection')


def new_connection_with_operations(
    get_document_text: Query[GetDocumentTextInput, GetDocumentTextOutput],
    create_document: Mutation[CreateDocumentInput, CreateDocumentOutput],
    replace_document_text: Mutation[ReplaceDocumentTextInput, ReplaceDocumentTextOutput],
    append_text: Mutation[AppendTextInput, AppendTextOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_document_text, create_document, replace_document_text, append_text)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.google_docs.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Google Docs stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'service_account_key', 'delegated_user', 'auth_method')):
        raise ValueError("Google Docs stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Google Docs credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Google Docs credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Google Docs credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Google Docs credential refresh_token has an invalid type")
    service_account_key = material.get("service_account_key")
    if service_account_key is not None and not (isinstance(service_account_key, str)):
        raise ValueError("Google Docs credential service_account_key has an invalid type")
    delegated_user = material.get("delegated_user")
    if delegated_user is not None and not (isinstance(delegated_user, str)):
        raise ValueError("Google Docs credential delegated_user has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Google Docs credential auth_method is invalid")
    method = selected_method or "google-oauth"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "google-oauth":
        if not oauth_client_id:
            raise ValueError("Google Docs credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Google Docs credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Google Docs credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Google Docs credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    elif method == "workspace-domain-delegation":
        if not service_account_key and not is_shared:
            raise ValueError("Google Docs credential service_account_key is required")
        if service_account_key:
            values["service_account_key"] = SecretString(service_account_key)
        if not delegated_user:
            raise ValueError("Google Docs credential delegated_user is required")
        if delegated_user:
            values["delegated_user"] = delegated_user
        if not access_token and is_shared:
            raise ValueError("Google Docs credential access_token is required")
        values["access_token"] = SecretString(access_token or '')
    else:
        raise ValueError("Google Docs credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "google-oauth"
    material['auth_method'] = method
    if method == "google-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
    elif method == "workspace-domain-delegation":
        if (credentials.service_account_key.reveal() if credentials.service_account_key is not None else ''):
            material["service_account_key"] = (credentials.service_account_key.reveal() if credentials.service_account_key is not None else '')
        if credentials.delegated_user:
            material["delegated_user"] = credentials.delegated_user
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
    else:
        raise ValueError("Google Docs credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Google Docs", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Google Docs connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Google Docs project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Google Docs project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.google_docs.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"docsEndpoint", "driveEndpoint", "maxResponseBytes", "maxTextBytes"}:
        raise ValueError("Google Docs configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "docsEndpoint" in value:
        docs_endpoint = value["docsEndpoint"]
        if not (isinstance(docs_endpoint, str)):
            raise ValueError("Google Docs configuration docsEndpoint has an invalid type")
        config_arguments['docs_endpoint'] = docs_endpoint
    if "driveEndpoint" in value:
        drive_endpoint = value["driveEndpoint"]
        if not (isinstance(drive_endpoint, str)):
            raise ValueError("Google Docs configuration driveEndpoint has an invalid type")
        config_arguments['drive_endpoint'] = drive_endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Google Docs configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxTextBytes" in value:
        max_text_bytes = value["maxTextBytes"]
        if not (type(max_text_bytes) is int):
            raise ValueError("Google Docs configuration maxTextBytes has an invalid type")
        config_arguments['max_text_bytes'] = max_text_bytes
    return Config(**config_arguments)
