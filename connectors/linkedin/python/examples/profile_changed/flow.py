# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Flow per change of the connected LinkedIn member's own profile: record it, read the profile back."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult, TriggerBindingDefinition, TriggerEvent

from dex_connectors.linkedin import (
    AuthenticatedProfile,
    Connection,
    GetAuthenticatedProfileInput,
    ProfileChangedEvent,
    ProfileField,
    define_profile_changed_trigger_binding,
    new_get_authenticated_profile_step,
)

CONNECTION_NAME = "linkedin-profile-watcher"
BINDING_NAME = "own-profile-changes"
FLOW_TYPE = "LinkedInProfileChangePython"


@dataclass(frozen=True, slots=True)
class ProfileChangeInput:
    """One profileChanged delivery: the stable Trigger event ID beside the delivered change."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: ProfileChangedEvent


@dataclass(frozen=True, slots=True)
class ProfileChangeOutcome:
    """The Flow result: the changed claims and the verified profile read back, when there is one."""

    subject: str
    changed_fields: tuple[str, ...] = field(metadata={"dex_json_name": "changedFields"})
    verified_profile: AuthenticatedProfile | None = field(
        default=None, metadata={"dex_json_name": "verifiedProfile", "dex_json_omit_default": True}
    )


profile_change_attribute = dex.Attribute("linkedin-profile-change", ProfileChangeInput)
profile_change_outcome_attribute = dex.Attribute("linkedin-profile-change-outcome", ProfileChangeOutcome)


def resolve_flow_id(event: TriggerEvent[ProfileChangedEvent]) -> str:
    """Name one Flow per change, so a redelivered change finds its existing Flow."""
    return f"linkedin-profile-change-{event.id}"


def map_to_flow_input(event: TriggerEvent[ProfileChangedEvent]) -> ProfileChangeInput:
    """Keep the Trigger event ID beside the delivered change."""
    return ProfileChangeInput(event.id, event.payload)


def accepts_every_profile_change(event: TriggerEvent[ProfileChangedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to email changes."""
    return True


def map_to_get_authenticated_profile_input(input: ProfileChangeInput) -> GetAuthenticatedProfileInput:
    """Return the provider Query input, which has no field."""
    return GetAuthenticatedProfileInput()


def validate_profile_change_input(input: ProfileChangeInput) -> str:
    """Return why a delivered change is unusable, or "" when it is valid."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not input.change.subject.strip():
        return "change.subject is required"
    if not input.change.changed_fields:
        return "change.changedFields is required"
    if any(name not in tuple(ProfileField) for name in input.change.changed_fields):
        return "change.changedFields names an unknown claim"
    return ""


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the verified profile, or none without a verified email, and complete the Flow."
class CompleteProfileChange(dex.Step[QueryResult[AuthenticatedProfile]]):
    """Record the read-back outcome and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteLinkedInProfileChange"

    def execute(self, context: dex.Context, input: QueryResult[AuthenticatedProfile]) -> dex.StepDecision:
        """Keep the verified profile only when LinkedIn loaded one."""
        change = profile_change_attribute.get(context)
        outcome = ProfileChangeOutcome(
            change.change.subject,
            change.change.changed_fields,
            input.value if input.branch == "profileLoaded" else None,
        )
        profile_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Validate and record the delivered profile change before reading the verified profile."
class RecordProfileChange(dex.Step[ProfileChangeInput]):
    """Validate and record the delivered change."""

    def __init__(self, read_verified_profile: dex.StepTarget[ProfileChangeInput]) -> None:
        """Bind the read-back Step this Step continues to."""
        self.read_verified_profile = read_verified_profile

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordLinkedInProfileChange"

    def execute(self, context: dex.Context, input: ProfileChangeInput) -> dex.StepDecision:
        """Fail an unusable delivery; record a usable one and read the profile back."""
        problem = validate_profile_change_input(input)
        if problem:
            return dex.force_fail(problem)
        profile_change_attribute.set(context, input)
        return dex.go_to(self.read_verified_profile, input)


class ProfileChangeFlow(dex.Flow[ProfileChangeInput]):
    """Record one change of the connected LinkedIn member's own profile."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static LinkedIn connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError(f"profile change Flow requires the {CONNECTION_NAME} connection")
        self.complete = CompleteProfileChange()
        self.read_verified_profile = new_get_authenticated_profile_step(
            step_type="ReadLinkedInVerifiedProfile",
            step_input_type=ProfileChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the member's verified profile back from LinkedIn, so the record holds "
                "the current verified email.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_authenticated_profile_input,
            profile_loaded=self.complete,
            # Business reaction: a change that unverified or removed the email is recorded without a
            # verified profile.
            verified_email_required=self.complete,
        )
        self.record = RecordProfileChange(self.read_verified_profile)

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "LinkedInProfileChangePython"

    def get_steps(self) -> dex.StepList[ProfileChangeInput]:
        """Start at the record Step, which the Trigger starts the Flow at."""
        return dex.StepList.start_step(self.record).other_steps(self.read_verified_profile, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(profile_change_attribute, profile_change_outcome_attribute)

    # Dex Web shows no field for this binding; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_profile_changed_trigger_binding(
                connection_name="linkedin-profile-watcher", binding_name="own-profile-changes"
            )
        ]
