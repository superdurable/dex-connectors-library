# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable OracleNetSuite Python example with a mock connection."""

import dex

from dex_connectors.oracle_netsuite.oraclenetsuitemock import Mock
from examples.customer_inspection.flow import FLOW_TYPE, CustomerFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on their Dex identities."""
    flow = CustomerFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
