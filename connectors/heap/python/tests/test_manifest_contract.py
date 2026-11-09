# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify the runnable Heap Flow uses generated identities."""

import dex

from dex_connectors.heap.heapmock import Mock
from examples.user_properties.flow import FLOW_TYPE, UserPropertiesFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = UserPropertiesFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
