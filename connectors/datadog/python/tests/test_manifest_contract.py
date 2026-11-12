# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Register the runnable Python example with a mock connection."""

import json
from pathlib import Path

import dex

import dex_connectors.datadog as datadog
from dex_connectors.datadog.datadogmock import Mock
from examples.datadog_lookup.flow import FLOW_TYPE, DatadogLookupFlow


def test_example_registers_with_mock_connection() -> None:
    flow = DatadogLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_list_monitors_input_schema_lets_callers_omit_defaulted_page_fields() -> None:
    document = json.loads((Path(datadog.__file__).parent / "dex-connector.json").read_text())
    schemas = {factory["operationId"]: factory["inputSchema"] for factory in document["factories"]}
    # Zero page selects the first page and zero pageSize selects 100.
    assert "required" not in schemas["listMonitors"]
