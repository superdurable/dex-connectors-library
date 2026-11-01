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
from dex_connectors.sdk import default_connector_step_execute_retry_policy

import dex_connectors.cloudflare as package
from dex_connectors.cloudflare import LIST_ZONES_DEFINITION, PURGE_CACHE_BY_URLS_DEFINITION
from dex_connectors.cloudflare.cloudflaremock import (
    Mock,
    list_zones_default_answer,
    purge_cache_by_urls_default_answer,
)
from examples.cloudflare_lookup.flow import FLOW_TYPE, CloudflareLookupFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    for definition, kind in ((LIST_ZONES_DEFINITION, "query"), (PURGE_CACHE_BY_URLS_DEFINITION, "mutation")):
        operation = operations[definition.operation.operation_id]
        assert operation["kind"] == kind
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=30)
        assert definition.step_defaults.execute_retry == default_connector_step_execute_retry_policy()
    zones = list_zones_default_answer().value
    purge = purge_cache_by_urls_default_answer().value
    assert zones is not None and purge is not None
    assert operations["listZones"]["mocks"][0]["output"] == {
        "success": zones.success,
        "result": [{"id": zone.id, "name": zone.name, "status": zone.status} for zone in zones.result],
    }
    assert operations["purgeCacheByURLs"]["mocks"][0]["output"] == {
        "success": purge.success,
        "result": {"id": purge.result.id},
    }


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
    flow = CloudflareLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
