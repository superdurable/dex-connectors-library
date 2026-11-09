# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable ZendeskSell Python example with a mock connection."""

import dex

from dex_connectors.zendesk_sell.sellmock import Mock
from examples.lead_discovery.flow import FLOW_TYPE, LeadDiscoveryFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on their Dex identities."""
    flow = LeadDiscoveryFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
