# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable ALAPI Flow with a mock connection."""

import dex

from dex_connectors.alapi.alapimock import Mock
from examples.ip_location.flow import IPLocationFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on Dex identities."""
    flow = IPLocationFlow(Mock().connection())
    assert flow.get_flow_type() == "ALAPIIPLocation"
    dex.Registry([flow], allow_async_handlers=True)
