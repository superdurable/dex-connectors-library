# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Nimble Python example with a mock connection."""

import dex

from dex_connectors.nimble.nimblemock import Mock
from examples.contact_id_discovery.flow import FLOW_TYPE, ContactIDDiscoveryFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on their Dex identities."""
    flow = ContactIDDiscoveryFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
