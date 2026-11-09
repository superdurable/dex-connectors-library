# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Iterable Python Flow with its generated connection."""

import dex

from dex_connectors.iterable.iterablemock import Mock
from examples.user_inspection.flow import FLOW_TYPE, UserInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = UserInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
