# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable ClickHouse Python Flow with its generated connection."""

import dex

from dex_connectors.clickhouse.clickhousemock import Mock
from examples.table_inspection.flow import FLOW_TYPE, TableInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = TableInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
