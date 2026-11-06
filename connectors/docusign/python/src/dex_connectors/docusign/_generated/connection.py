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
from .definitions import CONNECTOR_ID, CREATE_ENVELOPE_FROM_TEMPLATE_DEFINITION, DOWNLOAD_COMBINED_DOCUMENT_DEFINITION, GET_ENVELOPE_DEFINITION, LIST_ENVELOPE_RECIPIENTS_DEFINITION, VOID_ENVELOPE_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import CombinedDocument, CreateEnvelopeFromTemplateInput, CreatedEnvelope, DownloadCombinedDocumentInput, Envelope, EnvelopeRecipients, GetEnvelopeInput, ListEnvelopeRecipientsInput, VoidEnvelopeInput, VoidedEnvelope

class Connection:

    __slots__ = ('_create_envelope_from_template', '_get_envelope', '_list_envelope_recipients', '_void_envelope', '_download_combined_document', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        create_envelope_from_template: Mutation[CreateEnvelopeFromTemplateInput, CreatedEnvelope],
        get_envelope: Query[GetEnvelopeInput, Envelope],
        list_envelope_recipients: Query[ListEnvelopeRecipientsInput, EnvelopeRecipients],
        void_envelope: Mutation[VoidEnvelopeInput, VoidedEnvelope],
        download_combined_document: Query[DownloadCombinedDocumentInput, CombinedDocument],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or create_envelope_from_template is None or get_envelope is None or list_envelope_recipients is None or void_envelope is None or download_combined_document is None:
            raise ValueError("DocuSign eSignature connection provider and operations are required")
        if create_envelope_from_template.definition().operation != CREATE_ENVELOPE_FROM_TEMPLATE_DEFINITION.operation:
            raise ValueError("DocuSign eSignature connection operation identity differs")
        if get_envelope.definition().operation != GET_ENVELOPE_DEFINITION.operation:
            raise ValueError("DocuSign eSignature connection operation identity differs")
        if list_envelope_recipients.definition().operation != LIST_ENVELOPE_RECIPIENTS_DEFINITION.operation:
            raise ValueError("DocuSign eSignature connection operation identity differs")
        if void_envelope.definition().operation != VOID_ENVELOPE_DEFINITION.operation:
            raise ValueError("DocuSign eSignature connection operation identity differs")
        if download_combined_document.definition().operation != DOWNLOAD_COMBINED_DOCUMENT_DEFINITION.operation:
            raise ValueError("DocuSign eSignature connection operation identity differs")
        self.reference = reference
        self._create_envelope_from_template = create_envelope_from_template
        self._get_envelope = get_envelope
        self._list_envelope_recipients = list_envelope_recipients
        self._void_envelope = void_envelope
        self._download_combined_document = download_combined_document
        self._client = client

    def create_envelope_from_template(self) -> Mutation[CreateEnvelopeFromTemplateInput, CreatedEnvelope]:
        return self._create_envelope_from_template

    def get_envelope(self) -> Query[GetEnvelopeInput, Envelope]:
        return self._get_envelope

    def list_envelope_recipients(self) -> Query[ListEnvelopeRecipientsInput, EnvelopeRecipients]:
        return self._list_envelope_recipients

    def void_envelope(self) -> Mutation[VoidEnvelopeInput, VoidedEnvelope]:
        return self._void_envelope

    def download_combined_document(self) -> Query[DownloadCombinedDocumentInput, CombinedDocument]:
        return self._download_combined_document

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "docusign.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("DocuSign eSignature connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.docusign.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("DocuSign eSignature client is required")
    return Connection(reference, client.create_envelope_from_template(), client.get_envelope(), client.list_envelope_recipients(), client.void_envelope(), client.download_combined_document(), client)

setattr(new_connection, '__module__', 'dex_connectors.docusign.connection')


def new_connection_with_operations(
    create_envelope_from_template: Mutation[CreateEnvelopeFromTemplateInput, CreatedEnvelope],
    get_envelope: Query[GetEnvelopeInput, Envelope],
    list_envelope_recipients: Query[ListEnvelopeRecipientsInput, EnvelopeRecipients],
    void_envelope: Mutation[VoidEnvelopeInput, VoidedEnvelope],
    download_combined_document: Query[DownloadCombinedDocumentInput, CombinedDocument],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, create_envelope_from_template, get_envelope, list_envelope_recipients, void_envelope, download_combined_document)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.docusign.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("DocuSign eSignature stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'connect_hmac_key', 'auth_method')):
        raise ValueError("DocuSign eSignature stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("DocuSign eSignature credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("DocuSign eSignature credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("DocuSign eSignature credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("DocuSign eSignature credential refresh_token has an invalid type")
    connect_hmac_key = material.get("connect_hmac_key")
    if connect_hmac_key is not None and not (isinstance(connect_hmac_key, str)):
        raise ValueError("DocuSign eSignature credential connect_hmac_key has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("DocuSign eSignature credential auth_method is invalid")
    method = selected_method or "docusign-oauth"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "docusign-oauth":
        if not oauth_client_id:
            raise ValueError("DocuSign eSignature credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("DocuSign eSignature credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("DocuSign eSignature credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("DocuSign eSignature credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["connect_hmac_key"] = SecretString(connect_hmac_key or '')
    elif method == "docusign-developer-oauth":
        if not oauth_client_id:
            raise ValueError("DocuSign eSignature credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("DocuSign eSignature credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("DocuSign eSignature credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("DocuSign eSignature credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
        values["connect_hmac_key"] = SecretString(connect_hmac_key or '')
    else:
        raise ValueError("DocuSign eSignature credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "docusign-oauth"
    material['auth_method'] = method
    if method == "docusign-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if (credentials.connect_hmac_key.reveal() if credentials.connect_hmac_key is not None else ''):
            material["connect_hmac_key"] = (credentials.connect_hmac_key.reveal() if credentials.connect_hmac_key is not None else '')
    elif method == "docusign-developer-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
        if (credentials.connect_hmac_key.reveal() if credentials.connect_hmac_key is not None else ''):
            material["connect_hmac_key"] = (credentials.connect_hmac_key.reveal() if credentials.connect_hmac_key is not None else '')
    else:
        raise ValueError("DocuSign eSignature credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("DocuSign eSignature", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("DocuSign eSignature connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("DocuSign eSignature project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("DocuSign eSignature project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.docusign.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"accountId", "maxResponseBytes", "maxDocumentBytes", "connectMaxBodyBytes"}:
        raise ValueError("DocuSign eSignature configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "accountId" in value:
        account_id = value["accountId"]
        if not (isinstance(account_id, str)):
            raise ValueError("DocuSign eSignature configuration accountId has an invalid type")
        config_arguments['account_id'] = account_id
    else:
        config_arguments['account_id'] = ''
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("DocuSign eSignature configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxDocumentBytes" in value:
        max_document_bytes = value["maxDocumentBytes"]
        if not (type(max_document_bytes) is int):
            raise ValueError("DocuSign eSignature configuration maxDocumentBytes has an invalid type")
        config_arguments['max_document_bytes'] = max_document_bytes
    if "connectMaxBodyBytes" in value:
        connect_max_body_bytes = value["connectMaxBodyBytes"]
        if not (type(connect_max_body_bytes) is int):
            raise ValueError("DocuSign eSignature configuration connectMaxBodyBytes has an invalid type")
        config_arguments['connect_max_body_bytes'] = connect_max_body_bytes
    return Config(**config_arguments)
