# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable LinkedIn authenticated profile Flow."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.linkedin import (
    AuthenticatedProfile,
    Connection,
    GetAuthenticatedProfileInput,
    new_get_authenticated_profile_step,
)

CONNECTION_NAME = "linkedin-signup"


@dataclass(frozen=True, slots=True)
class ProfileInput:
    """No provider fields are needed for the authenticated member."""

    def to_operation_input(self) -> GetAuthenticatedProfileInput:
        """Build the empty UserInfo request."""
        return GetAuthenticatedProfileInput()


profile_attribute = dex.Attribute("linkedin-authenticated-profile", AuthenticatedProfile)


# dex:group group-id:linkedin-profile group-label:"LinkedIn Profile"
# dex:explanation text:"Persist the verified profile and complete the signup Flow."
class CompleteProfile(dex.Step[QueryResult[AuthenticatedProfile]]):
    """Save the verified UserInfo claims and complete the Flow."""

    def execute(self, context: dex.Context, input: QueryResult[AuthenticatedProfile]) -> dex.StepDecision:
        """Persist the public profile without credentials."""
        profile_attribute.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProfileFlow(dex.Flow[ProfileInput]):
    """Query LinkedIn through a static named connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the UserInfo Query before Worker registration."""
        self.complete = CompleteProfile()
        self.get_profile = new_get_authenticated_profile_step(
            step_type="GetAuthenticatedProfile",
            step_input_type=ProfileInput,
            annotations={
                "groupId": "linkedin-profile",
                "groupLabel": "LinkedIn Profile",
                "explanation": "Load the verified LinkedIn UserInfo profile.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=ProfileInput.to_operation_input,
            profile_loaded=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the example Flow identity stable."""
        return "LinkedInAuthenticatedProfilePython"

    def get_steps(self) -> dex.StepList[ProfileInput]:
        """Query and store the authenticated profile."""
        return dex.StepList.start_step(self.get_profile).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the result Attribute consumed by the Flow."""
        return dex.PersistenceSchema.of(profile_attribute)
