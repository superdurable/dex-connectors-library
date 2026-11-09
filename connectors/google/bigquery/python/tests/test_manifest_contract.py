# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable BigQuery Python example with a mock connection."""

import dex

from dex_connectors.google_bigquery.bigquerymock import Mock
from examples.datasets.flow import FLOW_TYPE, DatasetsFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on their Dex identities."""
    flow = DatasetsFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
