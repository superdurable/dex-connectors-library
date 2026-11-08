# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable PandaDoc Python Flow with its generated connection."""

import dex

from dex_connectors.pandadoc.pandadocmock import Mock
from examples.document_inspection.flow import FLOW_TYPE, DocumentInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = DocumentInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
