# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Greenhouse Python Flow with its generated connection."""

import dex
from dex_connectors.greenhouse.greenhousemock import Mock

from examples.candidate_inspection.flow import FLOW_TYPE, CandidateInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = CandidateInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
