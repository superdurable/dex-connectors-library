# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Facebook Lead Ads Python Flow with a mock connection."""

import dex

from dex_connectors.meta_facebook_lead_ads.facebookleadadsmock import Mock
from examples.lead_forms.flow import FLOW_TYPE, LeadFormsFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = LeadFormsFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
