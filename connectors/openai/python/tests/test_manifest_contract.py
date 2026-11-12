# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import json
import tomllib
from datetime import timedelta
from pathlib import Path
from typing import Any

import dex
import yaml
from dex_connectors.sdk import RetryPolicy, default_connector_step_execute_retry_policy

from dex_connectors.openai import CREATE_RESPONSE_DEFINITION, RETRIEVE_RESPONSE_DEFINITION, Response
from dex_connectors.openai.openaimock import (
    Mock,
    create_response_create_rejected,
    create_response_incomplete_summary,
    create_response_stored_summary,
    retrieve_response_not_found,
    retrieve_response_rejected,
    retrieve_response_stored_response,
)
from examples.summarize_text.flow import FLOW_TYPE, SummarizeTextFlow, SummaryModelConfiguration

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def response_wire(response: Response) -> dict[str, Any]:
    return {
        "id": response.id,
        "model": response.model,
        "status": response.status,
        "outputText": response.output_text,
        "usage": {
            "inputTokens": response.usage.input_tokens,
            "outputTokens": response.usage.output_tokens,
            "totalTokens": response.usage.total_tokens,
        },
    }


def test_manifest_operations_branches_and_defaults() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    package = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + package["project"]["version"]
    assert package["project"]["name"] == "dex-connector-openai"
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    for definition in (CREATE_RESPONSE_DEFINITION, RETRIEVE_RESPONSE_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
    create_defaults = CREATE_RESPONSE_DEFINITION.step_defaults
    assert create_defaults.execute_method_timeout == timedelta(seconds=150)
    assert create_defaults.execute_durability == 1
    assert create_defaults.execute_retry == RetryPolicy(
        timedelta(seconds=2), 2, timedelta(seconds=30), 3, timedelta(minutes=5)
    )
    retrieve_defaults = RETRIEVE_RESPONSE_DEFINITION.step_defaults
    assert retrieve_defaults.execute_method_timeout == timedelta(seconds=30)
    assert retrieve_defaults.execute_durability == 2
    assert retrieve_defaults.execute_retry == default_connector_step_execute_retry_policy()


def test_mocks_match_manifest() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    cases = {
        "storedSummary": create_response_stored_summary(),
        "incompleteSummary": create_response_incomplete_summary(),
        "createRejected": create_response_create_rejected(),
        "storedResponse": retrieve_response_stored_response(),
        "responseNotFound": retrieve_response_not_found(),
        "retrieveRejected": retrieve_response_rejected(),
    }
    mocks = [mock for operation in operations.values() for mock in operation["mocks"]]
    assert {mock["name"] for mock in mocks} == set(cases)
    for mock in mocks:
        case = cases[mock["name"]]
        assert case.branch == mock["branch"]
        if "output" in mock:
            assert case.value is not None and response_wire(case.value) == mock["output"]
        else:
            assert case.value == Response()
        if "failure" in mock:
            assert case.failure is not None
            assert {"kind": case.failure.kind.value, "message": case.failure.message} == mock["failure"]


def test_example_registers_with_mock_connection() -> None:
    flow = SummarizeTextFlow(Mock("openai-api").connection(), SummaryModelConfiguration(" gpt-6-luna "))
    assert flow.get_flow_type() == FLOW_TYPE
    assert flow.map_to_create_request(type("R", (), {"text": "hi"})()).model == "gpt-6-luna"  # type: ignore[arg-type]
    dex.Registry([flow], allow_async_handlers=True)


def test_create_response_input_schema_lets_callers_omit_the_model() -> None:
    schema_path = CONNECTOR_DIRECTORY / "python/src/dex_connectors/openai/dex-connector.json"
    document = json.loads(schema_path.read_text())
    schemas = {factory["operationId"]: factory["inputSchema"] for factory in document["factories"]}
    # An empty Model uses the connection's model; the key keeps its Go field spelling.
    create_schema = schemas["createResponse"]
    assert "Model" in create_schema["properties"]
    assert create_schema["required"] == ["Input", "Instructions", "StructuredOutput"]
