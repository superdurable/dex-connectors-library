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
