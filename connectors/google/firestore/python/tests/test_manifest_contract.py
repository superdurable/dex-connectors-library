# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Firestore Python example with a mock connection."""

import dex

from dex_connectors.google_firestore.firestoremock import Mock
from examples.documents.flow import FLOW_TYPE, DocumentsFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on their Dex identities."""
    flow = DocumentsFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
