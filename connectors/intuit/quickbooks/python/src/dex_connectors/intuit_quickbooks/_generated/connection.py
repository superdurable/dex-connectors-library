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
from .definitions import CONNECTOR_ID, CREATE_CUSTOMER_DEFINITION, CREATE_INVOICE_DEFINITION, FIND_CUSTOMER_DEFINITION, GET_INVOICE_DEFINITION, LIST_INVOICES_DEFINITION, RECORD_PAYMENT_DEFINITION, SEND_INVOICE_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import CreateCustomerInput, CreateCustomerOutput, CreateInvoiceInput, CreateInvoiceOutput, FindCustomerInput, FindCustomerOutput, GetInvoiceInput, Invoice, InvoicePage, ListInvoicesInput, Payment, RecordPaymentInput, SendInvoiceInput

class Connection:

    __slots__ = ('_find_customer', '_create_customer', '_create_invoice', '_get_invoice', '_list_invoices', '_send_invoice', '_record_payment', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        find_customer: Query[FindCustomerInput, FindCustomerOutput],
        create_customer: Mutation[CreateCustomerInput, CreateCustomerOutput],
        create_invoice: Mutation[CreateInvoiceInput, CreateInvoiceOutput],
        get_invoice: Query[GetInvoiceInput, Invoice],
        list_invoices: Query[ListInvoicesInput, InvoicePage],
        send_invoice: Mutation[SendInvoiceInput, Invoice],
        record_payment: Mutation[RecordPaymentInput, Payment],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or find_customer is None or create_customer is None or create_invoice is None or get_invoice is None or list_invoices is None or send_invoice is None or record_payment is None:
            raise ValueError("QuickBooks Online connection provider and operations are required")
        if find_customer.definition().operation != FIND_CUSTOMER_DEFINITION.operation:
            raise ValueError("QuickBooks Online connection operation identity differs")
        if create_customer.definition().operation != CREATE_CUSTOMER_DEFINITION.operation:
            raise ValueError("QuickBooks Online connection operation identity differs")
        if create_invoice.definition().operation != CREATE_INVOICE_DEFINITION.operation:
            raise ValueError("QuickBooks Online connection operation identity differs")
        if get_invoice.definition().operation != GET_INVOICE_DEFINITION.operation:
            raise ValueError("QuickBooks Online connection operation identity differs")
        if list_invoices.definition().operation != LIST_INVOICES_DEFINITION.operation:
            raise ValueError("QuickBooks Online connection operation identity differs")
        if send_invoice.definition().operation != SEND_INVOICE_DEFINITION.operation:
            raise ValueError("QuickBooks Online connection operation identity differs")
        if record_payment.definition().operation != RECORD_PAYMENT_DEFINITION.operation:
            raise ValueError("QuickBooks Online connection operation identity differs")
        self.reference = reference
        self._find_customer = find_customer
        self._create_customer = create_customer
        self._create_invoice = create_invoice
        self._get_invoice = get_invoice
        self._list_invoices = list_invoices
        self._send_invoice = send_invoice
        self._record_payment = record_payment
        self._client = client

    def find_customer(self) -> Query[FindCustomerInput, FindCustomerOutput]:
        return self._find_customer

    def create_customer(self) -> Mutation[CreateCustomerInput, CreateCustomerOutput]:
        return self._create_customer

    def create_invoice(self) -> Mutation[CreateInvoiceInput, CreateInvoiceOutput]:
        return self._create_invoice

    def get_invoice(self) -> Query[GetInvoiceInput, Invoice]:
        return self._get_invoice

    def list_invoices(self) -> Query[ListInvoicesInput, InvoicePage]:
        return self._list_invoices

    def send_invoice(self) -> Mutation[SendInvoiceInput, Invoice]:
        return self._send_invoice

    def record_payment(self) -> Mutation[RecordPaymentInput, Payment]:
        return self._record_payment

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "intuit_quickbooks.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("QuickBooks Online connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.intuit_quickbooks.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("QuickBooks Online client is required")
    return Connection(reference, client.find_customer(), client.create_customer(), client.create_invoice(), client.get_invoice(), client.list_invoices(), client.send_invoice(), client.record_payment(), client)

setattr(new_connection, '__module__', 'dex_connectors.intuit_quickbooks.connection')


def new_connection_with_operations(
    find_customer: Query[FindCustomerInput, FindCustomerOutput],
    create_customer: Mutation[CreateCustomerInput, CreateCustomerOutput],
    create_invoice: Mutation[CreateInvoiceInput, CreateInvoiceOutput],
    get_invoice: Query[GetInvoiceInput, Invoice],
    list_invoices: Query[ListInvoicesInput, InvoicePage],
    send_invoice: Mutation[SendInvoiceInput, Invoice],
    record_payment: Mutation[RecordPaymentInput, Payment],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, find_customer, create_customer, create_invoice, get_invoice, list_invoices, send_invoice, record_payment)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.intuit_quickbooks.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("QuickBooks Online stored credential does not match the connector credential fields")
    if set(material) - set(('client_id', 'client_secret', 'access_token', 'refresh_token', 'id_token')):
        raise ValueError("QuickBooks Online stored credential does not match the connector credential fields")
    is_shared = shared
    client_id = material.get("client_id")
    if client_id is not None and not (isinstance(client_id, str)):
        raise ValueError("QuickBooks Online credential client_id has an invalid type")
    client_secret = material.get("client_secret")
    if client_secret is not None and not (isinstance(client_secret, str)):
        raise ValueError("QuickBooks Online credential client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("QuickBooks Online credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("QuickBooks Online credential refresh_token has an invalid type")
    id_token = material.get("id_token")
    if id_token is not None and not (isinstance(id_token, str)):
        raise ValueError("QuickBooks Online credential id_token has an invalid type")
    if not client_id:
        raise ValueError("QuickBooks Online credential client_id is required")
    if not client_secret and not is_shared:
        raise ValueError("QuickBooks Online credential client_secret is required")
    if not access_token:
        raise ValueError("QuickBooks Online credential access_token is required")
    if not refresh_token and not is_shared:
        raise ValueError("QuickBooks Online credential refresh_token is required")
    return Credentials(client_id, SecretString(client_secret or ''), SecretString(access_token), SecretString(refresh_token or ''), SecretString(id_token or ''))


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    if credentials.client_id:
        material["client_id"] = credentials.client_id
    if credentials.client_secret.reveal():
        material["client_secret"] = credentials.client_secret.reveal()
    if credentials.access_token.reveal():
        material["access_token"] = credentials.access_token.reveal()
    if credentials.refresh_token.reveal():
        material["refresh_token"] = credentials.refresh_token.reveal()
    if credentials.id_token.reveal():
        material["id_token"] = credentials.id_token.reveal()
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("QuickBooks Online", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("QuickBooks Online connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("QuickBooks Online project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("QuickBooks Online project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.intuit_quickbooks.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"environment", "realmId", "maxResponseBytes"}:
        raise ValueError("QuickBooks Online configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "environment" in value and value["environment"] != '':
        environment = value["environment"]
        if not (isinstance(environment, str) and environment in ('production', 'sandbox')):
            raise ValueError("QuickBooks Online configuration environment has an invalid type")
        config_arguments['environment'] = environment
    if "realmId" in value and value["realmId"] != '':
        realm_id = value["realmId"]
        if not (isinstance(realm_id, str)):
            raise ValueError("QuickBooks Online configuration realmId has an invalid type")
        config_arguments['realm_id'] = realm_id
    else:
        config_arguments['realm_id'] = ''
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("QuickBooks Online configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
