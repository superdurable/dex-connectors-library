# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic tests of the issue_changed example and the project runner wiring."""

from datetime import UTC, datetime
from types import SimpleNamespace
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    StaticCredentialProvider,
    TriggerContext,
    TriggerEvent,
)
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConnectionConfiguration,
    ConnectionStore,
    InMemoryObjectStore,
    PollCursorStore,
    Scope,
    TriggerInbox,
)

from dex_connectors.atlassian_jira import (
    Client,
    Config,
    Issue,
    IssueChangedEvent,
    ProjectIssueChangedTriggerRoute,
    new_connection,
    new_project_issue_trigger_runner,
)
from examples.issue_changed.flow import (
    CONNECTION_NAME,
    ISSUE_CHANGED_TRIGGER_BINDING,
    IssueChangeFlow,
    IssueChangeInput,
    accepts_every_issue_change,
    map_to_flow_input,
    map_to_get_issue_input,
    resolve_flow_id,
    validate_issue_change,
)

SCOPE = Scope("jira-example", "preview")


def _event(change_kind: str = "created") -> TriggerEvent[IssueChangedEvent]:
    issue = Issue(id="20001", key="OPS-1", summary="Kickoff")
    payload = IssueChangedEvent("cloud", change_kind=change_kind, issue=issue)
    return TriggerEvent("jira-0123", datetime(2026, 11, 2, tzinfo=UTC), payload)


def test_flow_maps_each_issue_version_to_one_flow() -> None:
    event = _event()
    assert resolve_flow_id(event) == "jira-issue-change-jira-0123"
    assert accepts_every_issue_change(event)
    flow_input = map_to_flow_input(event)
    assert flow_input == IssueChangeInput("jira-0123", event.payload)
    # The stable ID survives a project move.
    assert map_to_get_issue_input(flow_input).issue_id_or_key == "20001"
    assert validate_issue_change(flow_input) == ""
    assert "changeKind" in validate_issue_change(map_to_flow_input(_event("deleted")))
    assert "triggerEventId" in validate_issue_change(IssueChangeInput(" ", event.payload))


def test_flow_declares_the_binding_and_its_project_picker() -> None:
    connection = new_connection(
        Client(Config(cloud_id=""), StaticCredentialProvider({})), ConnectionRef("atlassian", CONNECTION_NAME)
    )
    flow = IssueChangeFlow(connection)
    assert flow.get_flow_type() == "JiraIssueChangePython"
    [binding] = flow.get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, ISSUE_CHANGED_TRIGGER_BINDING)
    assert binding.configuration_ui is not None
    [unit] = binding.configuration_ui.units
    assert (unit.unit_id, unit.label) == ("projectPicker", "Watched project")
    assert "Leave it unsaved to watch every project" in unit.description
    assert [(item.port, item.json_pointer) for item in unit.bindings] == [
        ("projectId", "/projectId"),
        ("projectKey", "/projectKey"),
        ("projectName", "/projectName"),
    ]
    with pytest.raises(ValueError):
        IssueChangeFlow(new_connection(connection._client, ConnectionRef("atlassian", "other")))  # type: ignore[arg-type]


def _project(bindings: dict[str, Any]) -> Any:
    connection = ConnectionConfiguration(
        "jira",
        CONNECTION_NAME,
        "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go",
        "atlassian",
        {"cloudId": "1324a887-45db-1bf4-1e99-ef0ff456d421", "pollInterval": 15_000_000_000},
        "oauth2",
    )
    configuration = Configuration(
        SCOPE,
        1,
        (connection,),
        tuple(
            {
                "connectorId": "jira",
                "connectionName": CONNECTION_NAME,
                "triggerName": "issueChanged",
                "bindingName": name,
                "configuration": value,
            }
            for name, value in bindings.items()
        ),
    )
    objects = InMemoryObjectStore()
    return SimpleNamespace(
        configuration=configuration,
        connections=ConnectionStore(objects, SCOPE),
        trigger_inbox=lambda key: TriggerInbox(objects, SCOPE, key),
        poll_cursor_store=lambda key: PollCursorStore(objects, SCOPE, key),
    )


class _Target:
    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[IssueChangedEvent]) -> None:
        return None


async def test_project_runner_reads_saved_and_unsaved_bindings() -> None:
    project = _project({"saved": {"projectId": "10000", "projectKey": "OPS", "projectName": "Operations"}})
    http_client = httpx.AsyncClient()
    runner = await new_project_issue_trigger_runner(
        project,
        CONNECTION_NAME,
        [ProjectIssueChangedTriggerRoute(name, _Target()) for name in ("saved", "unsaved")],
        http_client=http_client,
    )
    assert runner.connection.reference.name == CONNECTION_NAME
    await runner.aclose()
    for routes, message in (
        ([], "routes are required"),
        ([ProjectIssueChangedTriggerRoute(" ", _Target())], "binding name and target"),
        ([ProjectIssueChangedTriggerRoute("a", _Target())] * 2, "duplicated"),
    ):
        with pytest.raises(ValueError, match=message):
            await new_project_issue_trigger_runner(project, CONNECTION_NAME, routes, http_client=http_client)
    invalid = _project({"saved": {"projectKey": "ops"}, "typo": {"project": "OPS"}})
    for name, message in (("saved", "uppercase"), ("typo", "unknown field")):
        with pytest.raises(ValueError, match=message):
            await new_project_issue_trigger_runner(
                invalid,
                CONNECTION_NAME,
                [ProjectIssueChangedTriggerRoute(name, _Target())],
                http_client=http_client,
            )
