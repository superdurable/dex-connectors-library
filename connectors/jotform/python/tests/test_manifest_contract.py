# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Jotform Python Flow with a mock connection."""

import dex

from dex_connectors.jotform.jotformmock import Mock
from examples.form_inspection.flow import FLOW_TYPE, FormInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = FormInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
