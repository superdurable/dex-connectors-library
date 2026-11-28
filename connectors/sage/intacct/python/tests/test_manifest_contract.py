# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Sage Intacct Python example with a mock connection."""

import dex

from dex_connectors.sage_intacct.sageintacctmock import Mock
from examples.invoice_inspection.flow import FLOW_TYPE, InvoiceFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on their Dex identities."""
    flow = InvoiceFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_lookup_only_binding_preserves_positional_calls() -> None:
    """Existing lookup-only scripts still register the original Flow."""
    import pytest

    from dex_connectors.sage_intacct import Connection, new_connection_with_operations

    mock = Mock()
    reference = mock.connection().reference
    for connection in (
        Connection(reference, mock.get_invoice_script),
        new_connection_with_operations(mock.get_invoice_script, reference),
    ):
        dex.Registry([InvoiceFlow(connection)], allow_async_handlers=True)
        assert connection.get_invoice() is mock.get_invoice_script
        with pytest.raises(ValueError, match="list_invoices"):
            connection.list_invoices()
