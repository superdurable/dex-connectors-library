# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A Flow started by the Jira issueChanged Trigger that reads each changed issue back from Jira."""

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

from dex_connectors.atlassian_jira import (
    ISSUE_CHANGE_CREATED,
    ISSUE_CHANGE_UPDATED,
    Connection,
    GetIssueInput,
    Issue,
    IssueChangedEvent,
    define_issue_changed_trigger_binding,
    new_get_issue_step,
)

CONNECTION_NAME = "jira-issue-watcher"
ISSUE_CHANGED_TRIGGER_BINDING = "project-issue-changes"
FLOW_TYPE = "JiraIssueChangePython"


@dataclass(frozen=True, slots=True)
class IssueChangeInput:
    """One issueChanged delivery and the stable Trigger event ID that also names the Flow."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: IssueChangedEvent


@dataclass(frozen=True, slots=True)
class IssueChangeOutcome:
    """The Flow result: the delivered change kind and the issue read back from Jira."""

    issue_key: str = field(metadata={"dex_json_name": "issueKey"})
    change_kind: str = field(metadata={"dex_json_name": "changeKind"})
    current: Issue


ISSUE_CHANGE = dex.Attribute("jira-issue-change", IssueChangeInput)
ISSUE_CHANGE_OUTCOME = dex.Attribute("jira-issue-change-outcome", IssueChangeOutcome)


def resolve_flow_id(event: TriggerEvent[IssueChangedEvent]) -> str:
    """Name one Flow per issue version; a redelivered change finds its existing Flow."""
    return f"jira-issue-change-{event.id}"


def map_to_flow_input(event: TriggerEvent[IssueChangedEvent]) -> IssueChangeInput:
    """Keep the Trigger event ID beside the delivered change."""
    return IssueChangeInput(trigger_event_id=event.id, change=event.payload)


def accepts_every_issue_change(event: TriggerEvent[IssueChangedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to new issues."""
    return True


def map_to_get_issue_input(input: IssueChangeInput) -> GetIssueInput:
    """Read the changed issue by its stable ID, which survives a project move."""
    return GetIssueInput(input.change.issue.id)


def validate_issue_change(input: IssueChangeInput) -> str:
    """Return why a delivered change is unusable, or an empty string."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not input.change.cloud_id.strip() or not input.change.issue.id.strip():
        return "change.cloudId and change.issue.id are required"
    if input.change.change_kind not in (ISSUE_CHANGE_CREATED, ISSUE_CHANGE_UPDATED):
        return "change.changeKind must be created or updated"
    return ""


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the delivered issue change, then read the issue back."
class RecordIssueChange(dex.Step[IssueChangeInput]):
    """Validate and record the delivered change."""

    def get_step_type(self) -> str:
        """Return the stable start Step identity."""
        return "RecordIssueChange"

    def execute(self, context: dex.Context, input: IssueChangeInput) -> dex.StepDecision:
        """Fail an unusable change; otherwise record it and read the issue."""
        problem = validate_issue_change(input)
        if problem:
            return dex.force_fail(problem)
        ISSUE_CHANGE.set(context, input)
        return dex.go_to(dex.StepRef("ReadChangedIssue"), input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the issue read back from Jira as the outcome and complete the Flow."
class CompleteIssueChange(dex.Step[QueryResult[Issue]]):
    """Record the issue read back as the outcome."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "CompleteIssueChange"

    def execute(self, context: dex.Context, input: QueryResult[Issue]) -> dex.StepDecision:
        """Save the outcome and complete with it."""
        change = ISSUE_CHANGE.get(context)
        outcome = IssueChangeOutcome(change.change.issue.key, change.change.change_kind, input.value)
        ISSUE_CHANGE_OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class IssueChangeFlow(dex.Flow[IssueChangeInput]):
    """Record one Jira issue change and read the issue back."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Jira connection at registration time."""
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("Jira issue change Flow requires its static connection name")
        self.record = RecordIssueChange()
        self.complete = CompleteIssueChange()
        # dex:group group-id:change group-label:"Change"
        # dex:explanation text:"Read the changed issue back, so the record holds its current state."
        self.read = new_get_issue_step(
            step_type="ReadChangedIssue",
            step_input_type=IssueChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed issue back, so the record holds its current state.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_issue_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "JiraIssueChangePython"

    def get_steps(self) -> dex.StepList[IssueChangeInput]:
        """Register the record, read, and completion Steps."""
        return dex.StepList.start_step(self.record).other_steps(self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(ISSUE_CHANGE, ISSUE_CHANGE_OUTCOME)

    # Declares the issueChanged binding and its project picker; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_issue_changed_trigger_binding(
                connection_name="jira-issue-watcher",
                binding_name="project-issue-changes",
                configuration_ui=ConnectorConfigurationUI(
                    units=(
                        ConnectorUIUnit(
                            id="watchedProject",
                            unit_id="projectPicker",
                            label="Watched project",
                            description="Choose the Jira project whose created and updated issues start this Flow; the list comes from this connection's Jira site and shows the projects the account can browse, and the unit saves the project ID, key, and name. Leave it unsaved to watch every project the account can browse. The first start records existing issues as a baseline and starts no Flow for them. Restart the Worker after saving; a changed project records a new baseline.",  # noqa: E501
                            required=False,
                            bindings=(
                                ConnectorUIBinding(port="projectId", json_pointer="/projectId"),
                                ConnectorUIBinding(port="projectKey", json_pointer="/projectKey"),
                                ConnectorUIBinding(port="projectName", json_pointer="/projectName"),
                            ),
                        ),
                    )
                ),
            )
        ]
