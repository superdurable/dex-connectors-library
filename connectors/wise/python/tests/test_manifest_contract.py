# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Wise Python Flow with its generated connection."""

import dex

from dex_connectors.wise.wisemock import Mock
from examples.transfer_inspection.flow import FLOW_TYPE, TransferInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = TransferInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
