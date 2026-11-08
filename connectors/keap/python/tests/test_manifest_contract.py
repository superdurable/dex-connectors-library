# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Keap Python Flow with its generated connection."""

import dex

from dex_connectors.keap.keapmock import Mock
from examples.contact_read.flow import FLOW_TYPE, ContactReadFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = ContactReadFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
