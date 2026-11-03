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
from dex_connectors.sdk.connectormock import Case

import dex_connectors.airtable as package
from dex_connectors.airtable import (
    GET_RECORD_DEFINITION,
    LIST_RECORDS_DEFINITION,
    UI_UNIT_BASE_PICKER,
    UI_UNIT_TABLE_PICKER,
    UPDATE_RECORDS_DEFINITION,
    UPSERT_RECORDS_DEFINITION,
    Record,
    RecordPage,
    UpdatedRecords,
    UpsertedRecords,
)
from dex_connectors.airtable.airtablemock import (
    Mock,
    get_record_approved_decision_log,
    get_record_decision_read_denied,
    get_record_missing_decision_log,
    list_records_expired_policy_offset,
    list_records_policy_list_denied,
    list_records_standard_refund_policy,
    update_records_missing_policy_stamp,
    update_records_policy_stamp_denied,
    update_records_stamped_standard_policy,
    upsert_records_decision_upsert_denied,
    upsert_records_logged_approved_refund,
)
from examples.policy_lookup.flow import FLOW_TYPE, PolicyLookupFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def record_wire(record: Record) -> dict[str, Any]:
    return {
        "id": record.id,
        "createdTime": record.created_time.strftime("%Y-%m-%dT%H:%M:%SZ"),
        "fields": record.fields,
    }


def value_wire(value: object) -> dict[str, Any]:
    if isinstance(value, Record):
        return record_wire(value)
    if isinstance(value, RecordPage):
        return {
            "records": [record_wire(record) for record in value.records],
            "offset": value.offset,
            "filterFormula": value.filter_formula,
        }
    if isinstance(value, UpsertedRecords):
        return {
            "records": [record_wire(record) for record in value.records],
            "createdRecordIds": list(value.created_record_ids),
            "updatedRecordIds": list(value.updated_record_ids),
        }
    assert isinstance(value, UpdatedRecords)
    return {"records": [record_wire(record) for record in value.records]}


def test_manifest_operations_branches_defaults_and_units() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    project = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + project["project"]["version"]
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    for definition, kind, durability in (
        (LIST_RECORDS_DEFINITION, "query", "async"),
        (GET_RECORD_DEFINITION, "query", "async"),
        (UPSERT_RECORDS_DEFINITION, "mutation", "sync"),
        (UPDATE_RECORDS_DEFINITION, "mutation", "async"),
    ):
        operation = operations[definition.operation.operation_id]
        assert operation["kind"] == kind
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": durability}
        assert definition.step_defaults.execute_method_timeout == timedelta(seconds=30)
        assert definition.step_defaults.execute_durability == (1 if durability == "sync" else 2)
        assert definition.step_defaults.execute_retry == default_connector_step_execute_retry_policy()
    assert [unit["id"] for unit in manifest["spec"]["studio"]["units"]] == [
        UI_UNIT_BASE_PICKER,
        UI_UNIT_TABLE_PICKER,
    ]


def test_mocks_match_manifest() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    cases: dict[tuple[str, str], Case[Any]] = {
        ("listRecords", "standardRefundPolicy"): list_records_standard_refund_policy(),
        ("listRecords", "expiredPolicyOffset"): list_records_expired_policy_offset(),
        ("listRecords", "policyListDenied"): list_records_policy_list_denied(),
        ("getRecord", "approvedDecisionLog"): get_record_approved_decision_log(),
        ("getRecord", "missingDecisionLog"): get_record_missing_decision_log(),
        ("getRecord", "decisionReadDenied"): get_record_decision_read_denied(),
        ("upsertRecords", "loggedApprovedRefund"): upsert_records_logged_approved_refund(),
        ("upsertRecords", "decisionUpsertDenied"): upsert_records_decision_upsert_denied(),
        ("updateRecords", "stampedStandardPolicy"): update_records_stamped_standard_policy(),
        ("updateRecords", "missingPolicyStamp"): update_records_missing_policy_stamp(),
        ("updateRecords", "policyStampDenied"): update_records_policy_stamp_denied(),
    }
    declared = {
        (operation["name"], mock["name"]): mock
        for operation in manifest["spec"]["operations"]
        for mock in operation["mocks"]
    }
    assert set(declared) == set(cases)
    for key, mock in declared.items():
        case = cases[key]
        assert case.branch == mock["branch"]
        if "output" in mock:
            assert value_wire(case.value) == mock["output"]
        if "pages" in mock:
            assert [value_wire(page) for page in case.pages or ()] == mock["pages"]
        if "failure" in mock:
            assert case.failure is not None
            assert {"kind": case.failure.kind.value, "message": case.failure.message} == mock["failure"]
            assert case.failure.operation == key[0]


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
    flow = PolicyLookupFlow(Mock("airtable-refund-policies").connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
