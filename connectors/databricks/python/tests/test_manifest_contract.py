# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Databricks Python Flow with its generated connection."""

import dex

from dex_connectors.databricks.databricksmock import Mock
from examples.job_run_inspection.flow import FLOW_TYPE, JobRunInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = JobRunInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
