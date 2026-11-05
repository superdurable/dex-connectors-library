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

import dex_connectors.google_gemini as package
from dex_connectors.google_gemini import (
    GENERATE_CONTENT_DEFINITION,
    UI_MODEL_PICKER_PORT_MODEL,
    UI_UNIT_MODEL_PICKER,
    GenerateContentResponse,
)
from dex_connectors.google_gemini.geminimock import (
    Mock,
    generate_content_launch_summary,
    generate_content_model_access_denied,
    generate_content_output_limit_reached,
    generate_content_unsafe_prompt_blocked,
)
from examples.generate_summary.flow import (
    FLOW_TYPE,
    GenerateSummaryFlow,
    SummaryModelConfiguration,
    SummaryRequest,
)

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def response_wire(response: GenerateContentResponse) -> dict[str, Any]:
    usage = {
        "promptTokens": response.usage.prompt_tokens,
        "candidateTokens": response.usage.candidate_tokens,
        "totalTokens": response.usage.total_tokens,
    }
    wire: dict[str, Any] = {
        "responseId": response.response_id,
        "modelVersion": response.model_version,
        "text": response.text,
        "usage": {name: value for name, value in usage.items() if value},
    }
    if response.finish_reason:
        wire["finishReason"] = response.finish_reason
    if response.block_reason:
        wire["blockReason"] = response.block_reason
    return wire


def test_manifest_operation_branches_and_defaults() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    assert project["project"]["name"] == "dex-connector-google-gemini"
    assert manifest["spec"]["provider"] == "google"
    (operation,) = manifest["spec"]["operations"]
    assert operation["name"] == GENERATE_CONTENT_DEFINITION.operation.operation_id
    assert [
        (branch["id"], branch["description"], branch.get("optional", False))
        for branch in operation["branches"]
    ] == [
        (branch.id, branch.description, branch.optional)
        for branch in GENERATE_CONTENT_DEFINITION.branches or ()
    ]
    execution = operation["execution"]
    assert (execution["executeMethodTimeout"], execution["heartbeatTimeout"], execution["durability"]) == (
        "300s",
        "300s",
        "sync",
    )
    defaults = GENERATE_CONTENT_DEFINITION.step_defaults
    assert defaults.execute_method_timeout == timedelta(seconds=300)
    assert defaults.heartbeat_timeout == timedelta(seconds=300)
    assert defaults.execute_durability == 1
    assert defaults.execute_retry == RetryPolicy(
        timedelta(seconds=2), 2, timedelta(seconds=60), 3, timedelta(minutes=10)
    )
    (unit,) = manifest["spec"]["studio"]["units"]
    assert unit["id"] == UI_UNIT_MODEL_PICKER
    assert [output["name"] for output in unit["outputs"]] == [UI_MODEL_PICKER_PORT_MODEL]


def test_mocks_match_manifest() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    (operation,) = manifest["spec"]["operations"]
    cases = {
        "launchSummary": generate_content_launch_summary(),
        "outputLimitReached": generate_content_output_limit_reached(),
        "unsafePromptBlocked": generate_content_unsafe_prompt_blocked(),
        "modelAccessDenied": generate_content_model_access_denied(),
    }
    assert {mock["name"] for mock in operation["mocks"]} == set(cases)
    for mock in operation["mocks"]:
        case = cases[mock["name"]]
        assert case.branch == mock["branch"]
        assert case.value is not None
        if "output" in mock:
            assert response_wire(case.value) == mock["output"]
        else:
            assert case.value == GenerateContentResponse()
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
        "configuration_ui",
        "result_attribute",
        "step_options_override",
    }
    for factory in document["factories"]:
        parameters = set(inspect.signature(getattr(package, factory["name"])).parameters)
        assert parameters - common == {branch["field"] for branch in factory["branches"]}


def test_example_registers_with_mock_connection() -> None:
    flow = GenerateSummaryFlow(
        Mock("gemini-api").connection(), SummaryModelConfiguration(" gemini-3.8-flash ")
    )
    assert flow.get_flow_type() == FLOW_TYPE
    request = flow.map_to_generate_content_request(SummaryRequest("Launch", "The connector shipped today."))
    assert request.model == "gemini-3.8-flash"
    assert request.contents[0].parts[0].text == "Title: Launch\n\nThe connector shipped today."
    dex.Registry([flow], allow_async_handlers=True)
