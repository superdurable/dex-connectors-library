# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Google Analytics 4 Python Flow with a mock connection."""

import json
from pathlib import Path

import dex

import dex_connectors.google_analytics as google_analytics
from dex_connectors.google_analytics.analytics4mock import Mock
from examples.property.flow import FLOW_TYPE, PropertyFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = PropertyFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_input_schemas_let_callers_omit_defaulted_page_sizes() -> None:
    document = json.loads((Path(google_analytics.__file__).parent / "dex-connector.json").read_text())
    schemas = {factory["operationId"]: factory["inputSchema"] for factory in document["factories"]}
    # Zero limit selects 100 rows and zero pageSize selects 50 audiences.
    assert "limit" not in schemas["runReport"]["required"]
    assert "pageSize" not in schemas["listAudiences"]["required"]
