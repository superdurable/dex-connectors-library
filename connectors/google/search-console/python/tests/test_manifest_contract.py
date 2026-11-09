# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Google Search Console Python Flow with a mock connection."""

import dex

from dex_connectors.google_search_console.searchconsolemock import Mock
from examples.sites.flow import FLOW_TYPE, SitesFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = SitesFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
