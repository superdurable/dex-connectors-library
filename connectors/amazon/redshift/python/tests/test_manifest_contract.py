# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Amazon Redshift Python Flow with its generated connection."""

import dex

from dex_connectors.amazon_redshift.redshiftmock import Mock
from examples.statement_results.flow import FLOW_TYPE, StatementResultsFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = StatementResultsFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
