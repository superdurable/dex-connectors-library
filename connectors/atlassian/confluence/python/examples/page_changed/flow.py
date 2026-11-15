# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A Flow started by the Confluence pageChanged Trigger that reads each changed page back as Markdown."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.atlassian_confluence import (
    PAGE_CHANGE_CREATED,
    PAGE_CHANGE_UPDATED,
    Connection,
    GetPageInput,
    Page,
    PageChangedEvent,
    define_page_changed_trigger_binding,
    new_get_page_step,
)

CONNECTION_NAME = "confluence-page-watcher"
PAGE_CHANGED_TRIGGER_BINDING = "space-page-changes"
FLOW_TYPE = "ConfluencePageChangePython"
MAX_RECORDED_BODY_CHARACTERS = 2000
"""Bounds the Markdown kept in the outcome."""


@dataclass(frozen=True, slots=True)
class PageChangeInput:
    """One pageChanged delivery and the stable Trigger event ID that also names the Flow."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: PageChangedEvent


@dataclass(frozen=True, slots=True)
class PageChangeOutcome:
    """The Flow result: the delivered change and the page read back from Confluence."""

    page_id: str = field(metadata={"dex_json_name": "pageId"})
    change_kind: str = field(metadata={"dex_json_name": "changeKind"})
    delivered_version: int = field(metadata={"dex_json_name": "deliveredVersion"})
    current_version: int = field(metadata={"dex_json_name": "currentVersion"})
    """Later than delivered_version when the page changed again before the read."""
    title: str
    body: str
    """The start of the page body as Markdown, at most MAX_RECORDED_BODY_CHARACTERS characters."""
    web_url: str = field(default="", metadata={"dex_json_name": "webUrl", "dex_json_omit_default": True})


PAGE_CHANGE = dex.Attribute("confluence-page-change", PageChangeInput)
PAGE_CHANGE_OUTCOME = dex.Attribute("confluence-page-change-outcome", PageChangeOutcome)


def resolve_flow_id(event: TriggerEvent[PageChangedEvent]) -> str:
    """Name one Flow per page version; a redelivered change finds its existing Flow."""
    return f"confluence-page-change-{event.id}"


def map_to_flow_input(event: TriggerEvent[PageChangedEvent]) -> PageChangeInput:
    """Keep the Trigger event ID beside the delivered change."""
    return PageChangeInput(trigger_event_id=event.id, change=event.payload)


def accepts_every_page_change(event: TriggerEvent[PageChangedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to new pages."""
    return True


def map_to_get_page_input(input: PageChangeInput) -> GetPageInput:
    """Read the changed page as Markdown, bounded for the record."""
    return GetPageInput(page_id=input.change.page.id, max_body_characters=MAX_RECORDED_BODY_CHARACTERS)


def validate_page_change(input: PageChangeInput) -> str:
    """Return why a delivered change is unusable, or an empty string."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not input.change.page.id.strip() or input.change.page.version_number < 1:
        return "change.page.id and change.page.versionNumber are required"
    if input.change.change_kind not in (PAGE_CHANGE_CREATED, PAGE_CHANGE_UPDATED):
        return "change.changeKind must be created or updated"
    return ""


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the delivered page change, then read the page back."
class RecordPageChange(dex.Step[PageChangeInput]):
    """Validate and record the delivered change."""

    def get_step_type(self) -> str:
        """Return the stable start Step identity."""
        return "RecordPageChange"

    def execute(self, context: dex.Context, input: PageChangeInput) -> dex.StepDecision:
        """Fail an unusable change; otherwise record it and read the page."""
        problem = validate_page_change(input)
        if problem:
            return dex.force_fail(problem)
        PAGE_CHANGE.set(context, input)
        return dex.go_to(dex.StepRef("ReadChangedPage"), input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the page read back from Confluence as the outcome and complete the Flow."
class CompletePageChange(dex.Step[QueryResult[Page]]):
    """Record the page read back as the outcome."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "CompletePageChange"

    def execute(self, context: dex.Context, input: QueryResult[Page]) -> dex.StepDecision:
        """Save the outcome and complete with it."""
        change = PAGE_CHANGE.get(context).change
        current = input.value
        outcome = PageChangeOutcome(
            page_id=change.page.id,
            change_kind=change.change_kind,
            delivered_version=change.page.version_number,
            current_version=current.version.number,
            title=current.title,
            body=current.body,
            web_url=current.web_url,
        )
        PAGE_CHANGE_OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class PageChangeFlow(dex.Flow[PageChangeInput]):
    """Record one Confluence page change and read the page back."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Confluence connection at registration time."""
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("Confluence page change Flow requires its static connection name")
        self.record = RecordPageChange()
        self.complete = CompletePageChange()
        # dex:group group-id:change group-label:"Change"
        # dex:explanation text:"Read the changed page back as Markdown, so the record holds its current body."
        self.read = new_get_page_step(
            step_type="ReadChangedPage",
            step_input_type=PageChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed page back as Markdown, so the record holds its current body.",  # noqa: E501
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_page_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "ConfluencePageChangePython"

    def get_steps(self) -> dex.StepList[PageChangeInput]:
        """Register the record, read, and completion Steps."""
        return dex.StepList.start_step(self.record).other_steps(self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(PAGE_CHANGE, PAGE_CHANGE_OUTCOME)

    # Declares the pageChanged binding and its space picker; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_page_changed_trigger_binding(
                connection_name="confluence-page-watcher",
                binding_name="space-page-changes",
                configuration_ui=ConnectorConfigurationUI(
                    units=(
                        ConnectorUIUnit(
                            id="watchedSpace",
                            unit_id="spacePicker",
                            label="Watched space",
                            description="Choose the Confluence space whose created and updated pages start this Flow; the list comes from the current spaces this connection's site lets the account view, and the picker saves the space ID, key, and name, which are not secret and are stored in the project configuration. The same spaces are listed at https://<your-site>.atlassian.net/wiki/spaces, and a key looks like OPS. Leave it unsaved to watch every space the account can view. The first start records recently changed pages as a baseline and starts no Flow for them. Restart the Worker after saving; a changed space records a new baseline.",  # noqa: E501
                            required=False,
                            bindings=(
                                ConnectorUIBinding(port="spaceId", json_pointer="/spaceId"),
                                ConnectorUIBinding(port="spaceKey", json_pointer="/spaceKey"),
                                ConnectorUIBinding(port="spaceName", json_pointer="/spaceName"),
                            ),
                        ),
                    )
                ),
            )
        ]
