# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Deel Python Flow with its generated connection."""

import dex

from dex_connectors.deel.deelmock import Mock
from examples.contract_inspection.flow import FLOW_TYPE, ContractInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = ContractInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
