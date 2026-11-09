# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Google Analytics 4 Python Flow with a mock connection."""

import dex

from dex_connectors.google_analytics.analytics4mock import Mock
from examples.property.flow import FLOW_TYPE, PropertyFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = PropertyFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
