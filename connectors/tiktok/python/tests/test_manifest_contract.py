# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable TikTok Python Flow with its generated connection."""

import dex

from dex_connectors.tiktok.tiktokmock import Mock
from examples.campaign_discovery.flow import FLOW_TYPE, CampaignDiscoveryFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = CampaignDiscoveryFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
