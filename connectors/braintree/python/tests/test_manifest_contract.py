# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Braintree Python Flow with its generated connection."""

import dex

from dex_connectors.braintree.braintreemock import Mock
from examples.transaction_read.flow import FLOW_TYPE, TransactionReadFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = TransactionReadFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
