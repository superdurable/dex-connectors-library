# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable GetResponse Python example with a mock connection."""

import dex

from dex_connectors.getresponse.getresponsemock import Mock
from examples.contact_inspection.flow import FLOW_TYPE, ContactInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on their Dex identities."""
    flow = ContactInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
