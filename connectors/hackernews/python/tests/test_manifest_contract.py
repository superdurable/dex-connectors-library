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

import dex_connectors.hackernews as package
from dex_connectors.hackernews import GET_ITEM_DEFINITION, LIST_STORY_IDS_DEFINITION, Item, StoryIDs
from dex_connectors.hackernews.hackernewsmock import (
    Mock,
    get_item_compiler_story,
    get_item_missing,
    get_item_rejected,
    list_story_ids_developer_feed,
    list_story_ids_feed_rejected,
)
from examples.top_story.flow import FLOW_TYPE, TopStoryFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def wire(value: Any) -> dict[str, Any]:
    if isinstance(value, StoryIDs):
        return {
            "ids": list(value.ids),
            "hasMore": value.has_more,
            "observedAt": value.observed_at.strftime("%Y-%m-%dT%H:%M:%SZ"),
        }
    assert isinstance(value, Item)
    return {
        "id": value.id,
        "type": value.type,
        "by": value.by,
        "time": value.time,
        "title": value.title,
        "url": value.url,
        "discussionUrl": value.discussion_url,
        "score": value.score,
        "descendants": value.descendants,
    }


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    cases: dict[str, Case[Any]] = {
        "developerFeed": list_story_ids_developer_feed(),
        "feedRejected": list_story_ids_feed_rejected(),
        "compilerStory": get_item_compiler_story(),
        "itemMissing": get_item_missing(),
        "itemRejected": get_item_rejected(),
    }
    for definition in (LIST_STORY_IDS_DEFINITION, GET_ITEM_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"]["executeMethodTimeout"] == "30s"
        assert operation["execution"]["retry"] == {
            "initialInterval": "1s",
            "backoffCoefficient": 2,
            "maximumInterval": "30s",
            "maximumAttempts": 0,
            "totalDuration": "65m",
        }
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=30)
        assert definition.step_defaults.execute_retry == RetryPolicy(
            timedelta(seconds=1), 2, timedelta(seconds=30), 0, timedelta(minutes=65)
        )
        for mock in operation["mocks"]:
            case = cases[mock["name"]]
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
    flow = TopStoryFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
