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
from .definitions import CONNECTOR_ID, CREATE_INVOICE_DEFINITION, FIND_CONTACT_BY_EMAIL_DEFINITION, GET_INVOICE_DEFINITION, LIST_CONTACTS_DEFINITION, LIST_INVOICES_DEFINITION, RECORD_PAYMENT_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import ContactPage, CreateInvoiceInput, CreateInvoiceOutput, FindContactByEmailInput, FindContactByEmailOutput, GetInvoiceInput, Invoice, InvoicePage, ListContactsInput, ListInvoicesInput, RecordPaymentInput, RecordPaymentOutput

class Connection:

    __slots__ = ('_list_contacts', '_find_contact_by_email', '_get_invoice', '_list_invoices', '_create_invoice', '_record_payment', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_contacts: Query[ListContactsInput, ContactPage],
        find_contact_by_email: Query[FindContactByEmailInput, FindContactByEmailOutput],
        get_invoice: Query[GetInvoiceInput, Invoice],
        list_invoices: Query[ListInvoicesInput, InvoicePage],
        create_invoice: Mutation[CreateInvoiceInput, CreateInvoiceOutput],
        record_payment: Mutation[RecordPaymentInput, RecordPaymentOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_contacts is None or find_contact_by_email is None or get_invoice is None or list_invoices is None or create_invoice is None or record_payment is None:
            raise ValueError("Xero connection provider and operations are required")
        if list_contacts.definition().operation != LIST_CONTACTS_DEFINITION.operation:
            raise ValueError("Xero connection operation identity differs")
        if find_contact_by_email.definition().operation != FIND_CONTACT_BY_EMAIL_DEFINITION.operation:
            raise ValueError("Xero connection operation identity differs")
        if get_invoice.definition().operation != GET_INVOICE_DEFINITION.operation:
            raise ValueError("Xero connection operation identity differs")
        if list_invoices.definition().operation != LIST_INVOICES_DEFINITION.operation:
            raise ValueError("Xero connection operation identity differs")
        if create_invoice.definition().operation != CREATE_INVOICE_DEFINITION.operation:
            raise ValueError("Xero connection operation identity differs")
        if record_payment.definition().operation != RECORD_PAYMENT_DEFINITION.operation:
            raise ValueError("Xero connection operation identity differs")
        self.reference = reference
        self._list_contacts = list_contacts
        self._find_contact_by_email = find_contact_by_email
        self._get_invoice = get_invoice
        self._list_invoices = list_invoices
        self._create_invoice = create_invoice
        self._record_payment = record_payment
        self._client = client

    def list_contacts(self) -> Query[ListContactsInput, ContactPage]:
        return self._list_contacts

    def find_contact_by_email(self) -> Query[FindContactByEmailInput, FindContactByEmailOutput]:
        return self._find_contact_by_email

    def get_invoice(self) -> Query[GetInvoiceInput, Invoice]:
        return self._get_invoice

    def list_invoices(self) -> Query[ListInvoicesInput, InvoicePage]:
        return self._list_invoices

    def create_invoice(self) -> Mutation[CreateInvoiceInput, CreateInvoiceOutput]:
        return self._create_invoice

    def record_payment(self) -> Mutation[RecordPaymentInput, RecordPaymentOutput]:
        return self._record_payment

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "xero.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Xero connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.xero.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Xero client is required")
    return Connection(reference, client.list_contacts(), client.find_contact_by_email(), client.get_invoice(), client.list_invoices(), client.create_invoice(), client.record_payment(), client)

setattr(new_connection, '__module__', 'dex_connectors.xero.connection')


def new_connection_with_operations(
    list_contacts: Query[ListContactsInput, ContactPage],
    find_contact_by_email: Query[FindContactByEmailInput, FindContactByEmailOutput],
    get_invoice: Query[GetInvoiceInput, Invoice],
    list_invoices: Query[ListInvoicesInput, InvoicePage],
    create_invoice: Mutation[CreateInvoiceInput, CreateInvoiceOutput],
    record_payment: Mutation[RecordPaymentInput, RecordPaymentOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_contacts, find_contact_by_email, get_invoice, list_invoices, create_invoice, record_payment)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.xero.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Xero stored credential does not match the connector credential fields")
    if set(material) - set(('client_id', 'client_secret', 'access_token', 'refresh_token', 'auth_method')):
        raise ValueError("Xero stored credential does not match the connector credential fields")
    is_shared = shared
    client_id = material.get("client_id")
    if client_id is not None and not (isinstance(client_id, str)):
        raise ValueError("Xero credential client_id has an invalid type")
    client_secret = material.get("client_secret")
    if client_secret is not None and not (isinstance(client_secret, str)):
        raise ValueError("Xero credential client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Xero credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Xero credential refresh_token has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Xero credential auth_method is invalid")
    method = selected_method or "custom-connection"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "custom-connection":
        if not client_id:
            raise ValueError("Xero credential client_id is required")
        if client_id:
            values["client_id"] = client_id
        if not client_secret and not is_shared:
            raise ValueError("Xero credential client_secret is required")
        if client_secret:
            values["client_secret"] = SecretString(client_secret)
        if not access_token and is_shared:
            raise ValueError("Xero credential access_token is required")
        values["access_token"] = SecretString(access_token or '')
    elif method == "xero-oauth":
        if not client_id:
            raise ValueError("Xero credential client_id is required")
        if client_id:
            values["client_id"] = client_id
        if not client_secret and not is_shared:
            raise ValueError("Xero credential client_secret is required")
        if client_secret:
            values["client_secret"] = SecretString(client_secret)
        if not access_token:
            raise ValueError("Xero credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Xero credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    else:
        raise ValueError("Xero credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "custom-connection"
    material['auth_method'] = method
    if method == "custom-connection":
        if credentials.client_id:
            material["client_id"] = credentials.client_id
        if (credentials.client_secret.reveal() if credentials.client_secret is not None else ''):
            material["client_secret"] = (credentials.client_secret.reveal() if credentials.client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
    elif method == "xero-oauth":
        if credentials.client_id:
            material["client_id"] = credentials.client_id
        if (credentials.client_secret.reveal() if credentials.client_secret is not None else ''):
            material["client_secret"] = (credentials.client_secret.reveal() if credentials.client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
    else:
        raise ValueError("Xero credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Xero", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Xero connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Xero project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Xero project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.xero.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes", "organisation"}:
        raise ValueError("Xero configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Xero configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "organisation" in value:
        organisation = value["organisation"]
        if not (isinstance(organisation, str)):
            raise ValueError("Xero configuration organisation has an invalid type")
        config_arguments['organisation'] = organisation
    else:
        config_arguments['organisation'] = ''
    return Config(**config_arguments)
