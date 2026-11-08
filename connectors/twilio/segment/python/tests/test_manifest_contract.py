# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Segment Python Flow with its generated connection."""

import dex

from dex_connectors.twilio_segment.segmentmock import Mock
from examples.source_inspection.flow import FLOW_TYPE, SourceInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = SourceInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
