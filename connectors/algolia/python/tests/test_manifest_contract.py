# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Register the runnable Python example with a mock connection."""

import json
from pathlib import Path

import dex

import dex_connectors.algolia as algolia
from dex_connectors.algolia.algoliamock import Mock
from examples.index_search.flow import FLOW_TYPE, IndexSearchFlow


def test_example_registers_with_mock_connection() -> None:
    flow = IndexSearchFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_search_input_schema_requires_only_the_index() -> None:
    document = json.loads((Path(algolia.__file__).parent / "dex-connector.json").read_text())
    schemas = {factory["operationId"]: factory["inputSchema"] for factory in document["factories"]}
    # A blank query matches all records and zero hitsPerPage uses Algolia's default.
    assert schemas["search"]["required"] == ["indexName"]
