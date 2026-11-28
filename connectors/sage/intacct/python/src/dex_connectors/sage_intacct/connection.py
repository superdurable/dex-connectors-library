# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Named Sage Intacct connections for complete clients and injected queries."""

import httpx
from dex_connectors.sdk import ConnectionRef, Query
from dex_connectors.sdk.projectconfig import LoadedProject

from ._generated import connection as generated
from ._generated.definitions import CONNECTOR_ID, GET_INVOICE_DEFINITION, LIST_INVOICES_DEFINITION
from .client import Client
from .models import GetInvoiceInput, InvoiceList, InvoiceSummary, ListInvoicesInput


class Connection:
    """Bind invoice queries outside Flow state. A lookup-only binding can serve lookup Steps."""

    __slots__ = ("_client", "_get_invoice", "_list_invoices", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        get_invoice: Query[GetInvoiceInput, InvoiceSummary],
        client: Client | None = None,
        *,
        list_invoices: Query[ListInvoicesInput, InvoiceList] | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_invoice is None:
            raise ValueError("Sage Intacct connection provider and lookup query are required")
        if get_invoice.definition().operation != GET_INVOICE_DEFINITION.operation:
            raise ValueError("Sage Intacct lookup query identity differs")
        if (
            list_invoices is not None
            and list_invoices.definition().operation != LIST_INVOICES_DEFINITION.operation
        ):
            raise ValueError("Sage Intacct list query identity differs")
        self.reference = reference
        self._get_invoice = get_invoice
        self._list_invoices = list_invoices
        self._client = client

    def get_invoice(self) -> Query[GetInvoiceInput, InvoiceSummary]:
        """Return the invoice lookup bound to this connection."""
        return self._get_invoice

    def list_invoices(self) -> Query[ListInvoicesInput, InvoiceList]:
        """Return the listing query, raising ValueError for a lookup-only binding."""
        if self._list_invoices is None:
            raise ValueError("Supply list_invoices to bind the invoice listing Step")
        return self._list_invoices

    async def aclose(self) -> None:
        """Close the client transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "sage_intacct.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Sage Intacct connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind both provider queries and close their transport with the connection."""
    if client is None:
        raise ValueError("Sage Intacct client is required")
    return Connection(reference, client.get_invoice(), client, list_invoices=client.list_invoices())


def new_connection_with_operations(
    get_invoice: Query[GetInvoiceInput, InvoiceSummary],
    reference: ConnectionRef,
    *,
    list_invoices: Query[ListInvoicesInput, InvoiceList] | None = None,
) -> Connection:
    """Bind supplied queries, preserving the positional lookup-only factory."""
    return Connection(reference, get_invoice, list_invoices=list_invoices)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Load a renewable project client and bind both invoice queries."""
    bound = generated.new_project_connection(project, connection_name, http_client)
    return Connection(
        bound.reference, bound.get_invoice(), bound._client, list_invoices=bound.list_invoices()
    )
