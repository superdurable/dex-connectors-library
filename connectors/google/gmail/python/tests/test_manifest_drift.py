# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Keep hand-written Python definitions and mocks aligned with connector.yaml."""

import re
import tomllib
from dataclasses import fields, is_dataclass
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import yaml
from dex_connectors.sdk import (
    MutationDefinition,
    QueryDefinition,
    default_connector_step_execute_retry_policy,
)

from dex_connectors.google_gmail import Config, Credentials, gmailmock
from dex_connectors.google_gmail.definitions import (
    GET_MESSAGE_DEFINITION,
    PROVIDER,
    REPLY_TO_MESSAGE_DEFINITION,
    SEND_MESSAGE_DEFINITION,
)
from dex_connectors.google_gmail.refresh import (
    GMAIL_SCOPES,
    GOOGLE_OAUTH_AUTH_METHOD_ID,
    GOOGLE_TOKEN_ENDPOINT,
    WORKSPACE_DOMAIN_DELEGATION_AUTH_METHOD_ID,
)

_CONNECTOR = Path(__file__).resolve().parents[2]
_DEFINITIONS: dict[str, QueryDefinition | MutationDefinition] = {
    "getMessage": GET_MESSAGE_DEFINITION,
    "sendMessage": SEND_MESSAGE_DEFINITION,
    "replyToMessage": REPLY_TO_MESSAGE_DEFINITION,
}


def _manifest() -> dict[str, Any]:
    return yaml.safe_load((_CONNECTOR / "connector.yaml").read_text())


def _seconds(value: str) -> timedelta:
    matched = re.fullmatch(r"([0-9]+)(s|m|h)", value)
    assert matched is not None, value
    multiplier = {"s": 1, "m": 60, "h": 3600}[matched.group(2)]
    return timedelta(seconds=int(matched.group(1)) * multiplier)


def _snake_case(value: str) -> str:
    first = re.sub(r"(.)([A-Z][a-z]+)", r"\1_\2", value)
    return re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", first).lower()


def _manifest_output(value: object) -> dict[str, object]:
    assert is_dataclass(value)
    output: dict[str, object] = {}
    for model_field in fields(value):
        wire_name = model_field.metadata.get("dex_json_name", model_field.name)
        field_value = getattr(value, model_field.name)
        if isinstance(field_value, datetime):
            output[wire_name] = field_value.astimezone(UTC).isoformat().replace("+00:00", "Z")
        else:
            output[wire_name] = field_value
    return output


def test_python_version_configuration_and_authorization_match_manifest() -> None:
    """Release, safe defaults, auth methods, credential fields, and scopes share one contract."""
    manifest = _manifest()
    project = tomllib.loads((_CONNECTOR / "python" / "pyproject.toml").read_text())["project"]
    spec = manifest["spec"]
    assert spec["languages"]["python"]["version"] == "v" + project["version"]
    assert manifest["metadata"]["name"] == "gmail"
    assert spec["provider"] == PROVIDER
    defaults = {field["name"]: field["default"] for field in spec["configuration"]["fields"]}
    config = Config()
    assert defaults == {
        "endpoint": config.endpoint,
        "maxResponseBytes": config.max_response_bytes,
        "maxMessageBytes": config.max_message_bytes,
        "pollInterval": f"{int(config.poll_interval_seconds)}s",
        "pollPageSize": config.poll_page_size,
    }
    auth = spec["auth"]
    methods = {method["id"]: method for method in auth["methods"]}
    assert auth["refreshable"] is True
    assert auth["defaultMethod"] == GOOGLE_OAUTH_AUTH_METHOD_ID
    assert set(methods) == {GOOGLE_OAUTH_AUTH_METHOD_ID, WORKSPACE_DOMAIN_DELEGATION_AUTH_METHOD_ID}
    assert methods[GOOGLE_OAUTH_AUTH_METHOD_ID]["type"] == "oauth2"
    assert methods[WORKSPACE_DOMAIN_DELEGATION_AUTH_METHOD_ID]["type"] == "serviceAccount"
    credential_fields = {field.name for field in fields(Credentials)} - {"auth_method_id"}
    assert credential_fields == {
        field["name"] for method in methods.values() for field in method["fields"]
    }
    oauth = methods[GOOGLE_OAUTH_AUTH_METHOD_ID]["oauth2"]
    delegated = methods[WORKSPACE_DOMAIN_DELEGATION_AUTH_METHOD_ID]["serviceAccount"]["jwtBearer"]
    assert oauth["tokenEndpoint"] == GOOGLE_TOKEN_ENDPOINT
    assert tuple(scope for scope in oauth["scopes"] if "/auth/gmail." in scope) == GMAIL_SCOPES
    assert tuple(delegated["scopes"]) == GMAIL_SCOPES
    assert _seconds(delegated["lifetime"]) == timedelta(hours=1)
    assert oauth["clientIDCredential"] == "oauth_client_id"
    assert oauth["clientSecretCredential"] == "oauth_client_secret"
    assert [(item["credential"], item["source"]) for item in oauth["credentialMappings"]] == [
        ("access_token", "access_token"),
        ("refresh_token", "refresh_token"),
    ]
    assert delegated["keyCredential"] == "service_account_key"
    assert delegated["subject"] == {"fromCredential": "delegated_user"}


def test_python_operation_definitions_match_manifest() -> None:
    """Every operation route, optional flag, and Execute default matches its manifest entry."""
    operations = {operation["name"]: operation for operation in _manifest()["spec"]["operations"]}
    assert set(operations) == set(_DEFINITIONS)
    for name, operation in operations.items():
        definition = _DEFINITIONS[name]
        assert definition.operation.connector_id == "gmail"
        assert definition.operation.operation_id == name
        assert ("mutation" if isinstance(definition, MutationDefinition) else "query") == operation["kind"]
        expected_idempotency = "required" if isinstance(definition, MutationDefinition) else "none"
        assert operation["idempotency"] == expected_idempotency
        assert definition.branches is not None
        assert [(branch.id, branch.description, branch.optional) for branch in definition.branches] == [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ]
        execution = operation["execution"]
        assert definition.step_defaults.execute_method_timeout == _seconds(execution["executeMethodTimeout"])
        assert definition.step_defaults.execute_durability == {"async": 2, "sync": 1}[
            execution["durability"]
        ]
        retry = definition.step_defaults.execute_retry
        assert retry is not None
        configured_retry = execution.get("retry")
        if configured_retry is None:
            assert retry == default_connector_step_execute_retry_policy()
        else:
            assert retry.initial_interval == _seconds(configured_retry["initialInterval"])
            assert retry.backoff_coefficient == configured_retry["backoffCoefficient"]
            assert retry.maximum_interval == _seconds(configured_retry["maximumInterval"])
            assert retry.maximum_attempts == configured_retry["maximumAttempts"]
            assert retry.total_duration == _seconds(configured_retry["totalDuration"])
            assert configured_retry["reason"]["kind"] == "duplicateWriteRisk"


def test_python_mock_factories_match_every_manifest_scenario() -> None:
    """Every declared mock factory returns the manifest's branch, failure, delay, and output."""
    operations = _manifest()["spec"]["operations"]
    expected_factory_names = {
        f"{_snake_case(operation['name'])}_{_snake_case(mock['name'])}"
        for operation in operations
        for mock in operation["mocks"]
    }
    assert set(gmailmock.__all__) - {"Mock"} == expected_factory_names
    for operation in operations:
        for mock in operation["mocks"]:
            factory_name = f"{_snake_case(operation['name'])}_{_snake_case(mock['name'])}"
            case = getattr(gmailmock, factory_name)()
            if "retry" in mock:
                assert case.kind == "retry" and not case.branch
                failure = mock["retry"]["failure"]
                assert case.retry_after_seconds == _seconds(mock["retry"].get("after", "0s")).total_seconds()
            else:
                assert case.kind == "branch" and case.branch == mock["branch"]
                failure = mock.get("failure")
                if "output" in mock:
                    assert _manifest_output(case.value) == mock["output"]
            if failure is None:
                assert case.failure is None
            else:
                assert case.failure is not None
                assert case.failure.kind.value == failure["kind"]
                assert case.failure.message == failure["message"]
                assert case.failure.provider == "gmail"
                assert case.failure.operation == operation["name"]
        assert sum(bool(mock.get("default")) for mock in operation["mocks"]) == 1
