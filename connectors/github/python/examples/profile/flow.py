# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable GitHub authenticated profile Flow."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.github import (
    AuthenticatedProfile,
    Connection,
    GetAuthenticatedProfileInput,
    new_get_authenticated_profile_step,
)

CONNECTION_NAME = "github-profile"


@dataclass(frozen=True, slots=True)
class ProfileRequest:
    """Safe request to inspect the connected GitHub account."""

    requested_by: str

    def to_operation_input(self) -> GetAuthenticatedProfileInput:
        """Map the Flow start input to the no-argument profile query."""
        return GetAuthenticatedProfileInput()


profile_result = dex.Attribute("github-profile-result", AuthenticatedProfile)


# dex:group group-id:github-profile group-label:"GitHub Profile"
# dex:explanation text:"Persist the verified public profile and complete the Flow."
class CompleteProfile(dex.Step[QueryResult[AuthenticatedProfile]]):
    """Retain the safe profile returned by GitHub."""

    def execute(self, context: dex.Context, input: QueryResult[AuthenticatedProfile]) -> dex.StepDecision:
        """Store the typed profile and complete the Flow."""
        profile_result.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProfileFlow(dex.Flow[ProfileRequest]):
    """Read the authenticated account with the static GitHub connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the connector Query before Worker registration."""
        self.complete = CompleteProfile()
        self.profile = new_get_authenticated_profile_step(
            step_type="GetAuthenticatedProfile",
            step_input_type=ProfileRequest,
            annotations={
                "groupId": "github-profile",
                "groupLabel": "GitHub Profile",
                "explanation": "Read the connected GitHub account and verified primary email.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=ProfileRequest.to_operation_input,
            profile_loaded=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "GitHubAuthenticatedProfilePython"

    def get_steps(self) -> dex.StepList[ProfileRequest]:
        """Start with the provider Query and complete on its happy path."""
        return dex.StepList.start_step(self.profile).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the profile Attribute used by the completion Step."""
        return dex.PersistenceSchema.of(profile_result)
