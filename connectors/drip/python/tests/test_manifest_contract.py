# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Drip Python example with a mock connection."""

import dex

from dex_connectors.drip.dripmock import Mock
from examples.account_discovery.flow import FLOW_TYPE, AccountDiscoveryFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on their Dex identities."""
    flow = AccountDiscoveryFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
