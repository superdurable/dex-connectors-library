# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Flow per Mailchimp contact change: record it and read the contact back."""

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

from dex_connectors.intuit_mailchimp import (
    Connection,
    GetMemberInput,
    Member,
    MemberChangedEvent,
    define_member_changed_trigger_binding,
    new_get_member_step,
)

CONNECTION_NAME = "mailchimp-audience"
BINDING_NAME = "audience-member-changes"
FLOW_TYPE = "MailchimpMemberChangePython"


@dataclass(frozen=True, slots=True)
class MemberChangeInput:
    """One memberChanged delivery: the stable Trigger event ID beside the delivered change."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: MemberChangedEvent


@dataclass(frozen=True, slots=True)
class MemberChangeOutcome:
    """The Flow result: the delivered contact and its current state read back from Mailchimp."""

    audience_id: str = field(metadata={"dex_json_name": "audienceId"})
    email_address: str = field(metadata={"dex_json_name": "emailAddress"})
    delivered_status: str = field(metadata={"dex_json_name": "deliveredStatus"})
    current: Member


member_change_attribute = dex.Attribute("mailchimp-member-change", MemberChangeInput)
member_change_outcome_attribute = dex.Attribute("mailchimp-member-change-outcome", MemberChangeOutcome)


def resolve_flow_id(event: TriggerEvent[MemberChangedEvent]) -> str:
    """Name one Flow per contact version, so a redelivered change finds its existing Flow."""
    return f"mailchimp-member-change-{event.id}"


def map_to_flow_input(event: TriggerEvent[MemberChangedEvent]) -> MemberChangeInput:
    """Keep the Trigger event ID beside the delivered change."""
    return MemberChangeInput(event.id, event.payload)


def accepts_every_member_change(event: TriggerEvent[MemberChangedEvent]) -> bool:
    """Admit every change; the binding's status picker already filters them."""
    return True


def map_to_get_member_input(input: MemberChangeInput) -> GetMemberInput:
    """Read the changed contact from its audience."""
    return GetMemberInput(list_id=input.change.audience_id, email_address=input.change.member.email_address)


def validate_member_change_input(input: MemberChangeInput) -> str:
    """Return why a delivered change is unusable, or "" when it is valid."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not input.change.audience_id.strip() or not input.change.member.email_address.strip():
        return "change.audienceId and change.member.emailAddress are required"
    return ""


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the contact read back from Mailchimp as the outcome and complete the Flow."
class CompleteMemberChange(dex.Step[QueryResult[Member]]):
    """Record the read-back outcome and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteMemberChange"

    def execute(self, context: dex.Context, input: QueryResult[Member]) -> dex.StepDecision:
        """Keep the delivered status beside the current contact."""
        change = member_change_attribute.get(context).change
        outcome = MemberChangeOutcome(
            change.audience_id, change.member.email_address, change.member.status, input.value
        )
        member_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Validate and record the delivered contact change, then read the contact back."
class RecordMemberChange(dex.Step[MemberChangeInput]):
    """Validate and record the delivered change."""

    def __init__(self, read_changed_member: dex.StepTarget[MemberChangeInput]) -> None:
        """Bind the read-back Step this Step continues to."""
        self.read_changed_member = read_changed_member

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordMemberChange"

    def execute(self, context: dex.Context, input: MemberChangeInput) -> dex.StepDecision:
        """Fail an unusable delivery; otherwise record it and read the contact back."""
        problem = validate_member_change_input(input)
        if problem:
            return dex.force_fail(problem)
        member_change_attribute.set(context, input)
        return dex.go_to(self.read_changed_member, input)


class MemberChangeFlow(dex.Flow[MemberChangeInput]):
    """Record one Mailchimp contact change and read the contact back."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Mailchimp connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError(f"member change Flow requires the {CONNECTION_NAME} connection")
        self.complete = CompleteMemberChange()
        self.read_changed_member = new_get_member_step(
            step_type="ReadChangedMember",
            step_input_type=MemberChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed contact back, so the record holds its current state rather "
                "than the polled copy.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_member_input,
            found=self.complete,
        )
        self.record = RecordMemberChange(self.read_changed_member)

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "MailchimpMemberChangePython"

    def get_steps(self) -> dex.StepList[MemberChangeInput]:
        """Start at the record Step, which the Trigger starts the Flow at."""
        return dex.StepList.start_step(self.record).other_steps(self.read_changed_member, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(member_change_attribute, member_change_outcome_attribute)

    # The analyzer reads the binding and its units from this lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_member_changed_trigger_binding(
                connection_name="mailchimp-audience",
                binding_name="audience-member-changes",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "watchedAudience",
                            "audienceIdEntry",
                            "Watched audience",
                            "Enter the ID of the audience whose added and changed contacts start this "
                            "Flow, such as 57afe96172, from Audience > More options > Audience settings in "
                            "Mailchimp. The first start records existing contacts as a baseline and starts "
                            "no Flow for them. Restart the Worker after saving; a changed audience records "
                            "a new baseline.",
                            True,
                            (ConnectorUIBinding("audienceId", "/audienceId"),),
                        ),
                        ConnectorUIUnit(
                            "deliveredStatuses",
                            "memberStatusPicker",
                            "Contact statuses",
                            "Select the statuses a contact must have after its change to start this Flow, "
                            "for example subscribed for new sign-ups or unsubscribed and cleaned to suppress "
                            "addresses elsewhere. Select none to start a Flow for every changed contact.",
                            False,
                            (ConnectorUIBinding("statuses", "/statuses"),),
                        ),
                    )
                ),
            )
        ]
