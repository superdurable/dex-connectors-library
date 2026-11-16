# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic tests of the page_changed example and the project runner wiring."""

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

from dex_connectors.atlassian_confluence import (
    Client,
    Config,
    PageChangedEvent,
    PageSummary,
    ProjectPageChangedTriggerRoute,
    new_connection,
    new_project_page_trigger_runner,
)
from examples.page_changed.flow import (
    CONNECTION_NAME,
    MAX_RECORDED_BODY_CHARACTERS,
    PAGE_CHANGED_TRIGGER_BINDING,
    PageChangeFlow,
    PageChangeInput,
    accepts_every_page_change,
    map_to_flow_input,
    map_to_get_page_input,
    resolve_flow_id,
    validate_page_change,
)

SCOPE = Scope("confluence-example", "preview")


def _event(change_kind: str = "created", version_number: int = 1) -> TriggerEvent[PageChangedEvent]:
    page = PageSummary(
        id="600001", title="Remote work policy", space_key="OPS", version_number=version_number
    )
    payload = PageChangedEvent("cloud", change_kind=change_kind, page=page)
    return TriggerEvent("confluence-page-0123", datetime(2026, 11, 2, tzinfo=UTC), payload)


def test_flow_maps_each_page_version_to_one_flow() -> None:
    event = _event()
    assert resolve_flow_id(event) == "confluence-page-change-confluence-page-0123"
    assert accepts_every_page_change(event)
    flow_input = map_to_flow_input(event)
    assert flow_input == PageChangeInput("confluence-page-0123", event.payload)
    read = map_to_get_page_input(flow_input)
    assert (read.page_id, read.max_body_characters) == ("600001", MAX_RECORDED_BODY_CHARACTERS)
    assert validate_page_change(flow_input) == ""
    assert "changeKind" in validate_page_change(map_to_flow_input(_event("deleted")))
    assert "versionNumber" in validate_page_change(map_to_flow_input(_event(version_number=0)))
    assert "triggerEventId" in validate_page_change(PageChangeInput(" ", event.payload))


def test_flow_declares_the_binding_and_its_space_picker() -> None:
    connection = new_connection(
        Client(Config(cloud_id=""), StaticCredentialProvider({})), ConnectionRef("atlassian", CONNECTION_NAME)
    )
    flow = PageChangeFlow(connection)
    assert flow.get_flow_type() == "ConfluencePageChangePython"
    [binding] = flow.get_connector_trigger_bindings()
    assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, PAGE_CHANGED_TRIGGER_BINDING)
    assert binding.configuration_ui is not None
    [unit] = binding.configuration_ui.units
    assert (unit.unit_id, unit.label) == ("spacePicker", "Watched space")
    assert "Leave it unsaved to watch every space" in unit.description
    assert [(item.port, item.json_pointer) for item in unit.bindings] == [
        ("spaceId", "/spaceId"),
        ("spaceKey", "/spaceKey"),
        ("spaceName", "/spaceName"),
    ]
    with pytest.raises(ValueError):
        PageChangeFlow(new_connection(connection._client, ConnectionRef("atlassian", "other")))  # type: ignore[arg-type]


def _project(bindings: dict[str, Any]) -> Any:
    connection = ConnectionConfiguration(
        "confluence",
        CONNECTION_NAME,
        "github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/go",
        "atlassian",
        {"cloudId": "1324a887-45db-1bf4-1e99-ef0ff456d421", "pollInterval": 30_000_000_000},
        "oauth2",
    )
    configuration = Configuration(
        SCOPE,
        1,
        (connection,),
        tuple(
            {
                "connectorId": "confluence",
                "connectionName": CONNECTION_NAME,
                "triggerName": "pageChanged",
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
    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[PageChangedEvent]) -> None:
        return None


async def test_project_runner_reads_saved_and_unsaved_bindings() -> None:
    project = _project({"saved": {"spaceId": "98306", "spaceKey": "OPS", "spaceName": "Operations"}})
    http_client = httpx.AsyncClient()
    runner = await new_project_page_trigger_runner(
        project,
        CONNECTION_NAME,
        [ProjectPageChangedTriggerRoute(name, _Target()) for name in ("saved", "unsaved")],
        http_client=http_client,
    )
    assert runner.connection.reference.name == CONNECTION_NAME
    await runner.aclose()
    for routes, message in (
        ([], "routes are required"),
        ([ProjectPageChangedTriggerRoute(" ", _Target())], "binding name and target"),
        ([ProjectPageChangedTriggerRoute("a", _Target())] * 2, "duplicated"),
    ):
        with pytest.raises(ValueError, match=message):
            await new_project_page_trigger_runner(project, CONNECTION_NAME, routes, http_client=http_client)
    invalid = _project({"saved": {"spaceKey": "bad key"}, "typo": {"space": "OPS"}})
    for name, message in (("saved", "space key"), ("typo", "unknown field")):
        with pytest.raises(ValueError, match=message):
            await new_project_page_trigger_runner(
                invalid,
                CONNECTION_NAME,
                [ProjectPageChangedTriggerRoute(name, _Target())],
                http_client=http_client,
            )
