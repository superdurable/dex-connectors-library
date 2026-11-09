# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Marketo Python Flow with its generated connection."""

import dex

from dex_connectors.adobe_marketo.adobe_marketomock import Mock
from examples.lead_inspection.flow import FLOW_TYPE, LeadInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = LeadInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
