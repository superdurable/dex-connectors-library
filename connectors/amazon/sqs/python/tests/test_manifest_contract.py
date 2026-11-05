# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import inspect
import json
import tomllib
from datetime import timedelta
from pathlib import Path

import dex
import yaml
from dex_connectors.sdk import RetryPolicy, default_connector_step_execute_retry_policy

import dex_connectors.amazon_sqs as package
from dex_connectors.amazon_sqs import (
    CHANGE_MESSAGE_VISIBILITY_DEFINITION,
    DELETE_MESSAGE_DEFINITION,
    RECEIVE_MESSAGES_DEFINITION,
    sqsmock,
)
from examples.queue_consumer.flow import CONNECTION_NAME, FLOW_TYPE, QueueConsumerFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def _duration(value: str) -> timedelta:
    units = {"s": 1, "m": 60}
    return timedelta(seconds=int(value[:-1]) * units[value[-1]])


def test_manifest_operations_branches_and_defaults() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    python = manifest["spec"]["languages"]["python"]
    assert python["version"] == "v" + project["project"]["version"]
    assert python["triggers"] == []
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    assert python["operations"] == list(operations)
    for definition, kind in (
        (RECEIVE_MESSAGES_DEFINITION, "mutation"),
        (DELETE_MESSAGE_DEFINITION, "mutation"),
        (CHANGE_MESSAGE_VISIBILITY_DEFINITION, "mutation"),
    ):
        operation = operations[definition.operation.operation_id]
        assert operation["kind"] == kind
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        execution = operation["execution"]
        defaults = definition.step_defaults
        assert defaults.execute_method_timeout == _duration(execution["executeMethodTimeout"])
        assert defaults.execute_durability == (1 if execution["durability"] == "sync" else 2)
        retry = execution.get("retry")
        if retry is None:
            assert defaults.execute_retry == default_connector_step_execute_retry_policy()
        else:
            assert defaults.execute_retry == RetryPolicy(
                _duration(retry["initialInterval"]),
                retry["backoffCoefficient"],
                _duration(retry["maximumInterval"]),
                retry["maximumAttempts"],
                _duration(retry["totalDuration"]),
            )


def test_mock_defaults_match_manifest_default_mocks() -> None:
    received = sqsmock.receive_messages_paid_order().value
    assert received is not None and received.messages[0].md5_of_body == "457ba27172a5858e4fed7600ec948c39"


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
    flow = QueueConsumerFlow(sqsmock.Mock(CONNECTION_NAME).connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
