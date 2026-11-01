# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import inspect
import json
import tomllib
from datetime import timedelta
from pathlib import Path
from typing import Any

import dex
import yaml
from dex_connectors.sdk import RetryPolicy
from dex_connectors.sdk.connectormock import Case

import dex_connectors.cohere as package
from dex_connectors.cohere import EMBED_DEFINITION, RERANK_DEFINITION, EmbedOutput, RerankOutput
from dex_connectors.cohere.coheremock import (
    Mock,
    embed_default_embedding,
    embed_invalid_key,
    rerank_default_ranking,
    rerank_invalid_key,
)
from examples.cohere_search.flow import FLOW_TYPE, SearchFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def wire(value: Any) -> dict[str, Any]:
    if isinstance(value, EmbedOutput):
        return {"id": value.id, "embeddings": [list(vector) for vector in value.embeddings]}
    assert isinstance(value, RerankOutput)
    return {
        "results": [{"index": item.index, "relevanceScore": item.relevance_score} for item in value.results]
    }


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    cases: dict[str, dict[str, Case[Any]]] = {
        "embed": {"defaultEmbedding": embed_default_embedding(), "invalidKey": embed_invalid_key()},
        "rerank": {"defaultRanking": rerank_default_ranking(), "invalidKey": rerank_invalid_key()},
    }
    for definition in (EMBED_DEFINITION, RERANK_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        execution = operation["execution"]
        assert (execution["executeMethodTimeout"], execution["durability"]) == ("45s", "async")
        retry = execution["retry"]
        assert (
            retry["initialInterval"],
            retry["maximumInterval"],
            retry["maximumAttempts"],
            retry["totalDuration"],
        ) == (
            "2s",
            "30s",
            3,
            "5m",
        )
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=45)
        assert definition.step_defaults.execute_retry == RetryPolicy(
            timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(minutes=5)
        )
        for mock in operation["mocks"]:
            case = cases[operation["name"]][mock["name"]]
            assert case.branch == mock["branch"]
            if "output" in mock:
                assert wire(case.value) == mock["output"]
            if "failure" in mock:
                assert case.failure is not None
                assert {"kind": case.failure.kind.value, "message": case.failure.message} == mock["failure"]


def test_factory_parameters_match_declared_branch_fields() -> None:
    document = json.loads((Path(package.__file__).parent / "dex-connector.json").read_text())
    common = {
        "step_type",
        "step_input_type",
        "annotations",
        "connection",
        "connection_name",
        "map_to_operation_input",
        "result_attribute",
        "step_options_override",
    }
    for factory in document["factories"]:
        parameters = set(inspect.signature(getattr(package, factory["name"])).parameters)
        assert parameters - common == {branch["field"] for branch in factory["branches"]}


def test_example_registers_with_mock_connection() -> None:
    flow = SearchFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
