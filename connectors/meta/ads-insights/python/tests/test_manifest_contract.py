# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Meta Ads Insights Python Flow with a mock connection."""

import dex

from dex_connectors.meta_ads_insights.metaadsinsightsmock import Mock
from examples.ad_inspection.flow import FLOW_TYPE, AdInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = AdInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
