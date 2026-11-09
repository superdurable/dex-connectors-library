# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Microsoft Dynamics 365 Sales Python Flow with its generated connection."""

import dex

from dex_connectors.microsoft_dynamics_365_sales.dynamics365salesmock import Mock
from examples.contact_lookup.flow import FLOW_TYPE, ContactFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = ContactFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
