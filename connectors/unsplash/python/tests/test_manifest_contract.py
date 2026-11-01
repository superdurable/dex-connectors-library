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
from dex_connectors.sdk import default_connector_step_execute_retry_policy

import dex_connectors.unsplash as package
from dex_connectors.unsplash import GET_PHOTO_DEFINITION, SEARCH_PHOTOS_DEFINITION, Photo
from dex_connectors.unsplash.unsplashmock import Mock, get_photo_default_answer, search_photos_default_answer
from examples.photo_search.flow import FLOW_TYPE, PhotoSearchFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def photo_wire(photo: Photo) -> dict[str, Any]:
    return {
        "id": photo.id,
        "description": photo.description,
        "altDescription": photo.alt_description,
        "htmlUrl": photo.html_url,
        "regularUrl": photo.regular_url,
        "photographerName": photo.photographer_name,
        "photographerURL": photo.photographer_url,
    }


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    for definition in (SEARCH_PHOTOS_DEFINITION, GET_PHOTO_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=30)
        assert definition.step_defaults.execute_retry == default_connector_step_execute_retry_policy()
    page = search_photos_default_answer().value
    photo = get_photo_default_answer().value
    assert page is not None and photo is not None
    assert operations["searchPhotos"]["mocks"][0]["output"] == {
        "total": page.total,
        "totalPages": page.total_pages,
        "photos": [photo_wire(item) for item in page.photos],
    }
    assert operations["getPhoto"]["mocks"][0]["output"] == photo_wire(photo)


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
    flow = PhotoSearchFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
