# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Google Tag Manager Python Flow with a mock connection."""

import dex

from dex_connectors.google_tag_manager.tagmanagermock import Mock
from examples.accounts.flow import FLOW_TYPE, AccountsFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = AccountsFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
