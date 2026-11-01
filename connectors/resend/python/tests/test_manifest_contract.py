# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import json
import tomllib
from datetime import timedelta
from pathlib import Path

import yaml
from dex_connectors.sdk import default_connector_step_execute_retry_policy

from dex_connectors.resend import GET_EMAIL_DEFINITION, SEND_EMAIL_DEFINITION
from dex_connectors.resend.resendmock import get_email_default, send_email_default

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]
PACKAGE_DIRECTORY = CONNECTOR_DIRECTORY / "python" / "src" / "dex_connectors" / "resend"


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    package = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + package["project"]["version"]
    assert package["project"]["name"] == "dex-connector-resend"
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    assert set(operations) == {"sendEmail", "getEmail"}
    for definition, kind in ((SEND_EMAIL_DEFINITION, "mutation"), (GET_EMAIL_DEFINITION, "query")):
        operation = operations[definition.operation.operation_id]
        assert definition.operation.connector_id == manifest["metadata"]["name"]
        assert operation["kind"] == kind
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
        defaults = definition.step_defaults
        assert defaults.execute_method_timeout == timedelta(seconds=30)
        assert defaults.execute_durability == 2
        assert defaults.execute_retry == default_connector_step_execute_retry_policy()
    sent_mock = operations["sendEmail"]["mocks"][0]
    assert sent_mock["branch"] == send_email_default().branch
    sent = send_email_default().value
    assert sent is not None and sent_mock["output"] == {"id": sent.id}
    found_mock = operations["getEmail"]["mocks"][0]
    found = get_email_default().value
    assert found is not None and found_mock["branch"] == get_email_default().branch
    assert found_mock["output"] == {
        "id": found.id,
        "from": found.from_address,
        "to": list(found.to),
        "subject": found.subject,
        "created_at": found.created_at,
    }


def test_dex_connector_package_identity_and_factories() -> None:
    document = json.loads((PACKAGE_DIRECTORY / "dex-connector.json").read_text())
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    assert document["releaseTag"] == "connectors/resend/go/" + manifest["metadata"]["version"]
    assert document["package"]["version"] == manifest["spec"]["languages"]["python"]["version"][1:]
    for factory, definition in zip(
        document["factories"], (SEND_EMAIL_DEFINITION, GET_EMAIL_DEFINITION), strict=True
    ):
        assert factory["operationId"] == definition.operation.operation_id
        assert [(branch["id"], branch["optional"]) for branch in factory["branches"]] == [
            (branch.id, branch.optional) for branch in definition.branches or ()
        ]
