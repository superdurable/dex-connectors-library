# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Chargebee Python Flow with its generated connection."""

import dex

from dex_connectors.chargebee.chargebeemock import Mock
from examples.subscription_inspection.flow import FLOW_TYPE, SubscriptionInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = SubscriptionInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
