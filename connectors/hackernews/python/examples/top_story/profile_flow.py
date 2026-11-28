# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read a public Hacker News profile by case-sensitive username."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.hackernews import Connection, GetUserInput, User, new_get_user_step

CONNECTION_NAME = "hackernews"


@dataclass(frozen=True)
class ProfileRequest:
    """Public username as displayed on the Hacker News profile."""

    username: str


def profile_input(request: ProfileRequest) -> GetUserInput:
    """Use the selected username for the Firebase query."""
    return GetUserInput(id=request.username)


# dex:group group-id:profile group-label:"Public profile"
# dex:explanation text:"Start a profile lookup for the selected username."
class StartProfile(dex.Step[ProfileRequest]):
    def get_step_type(self) -> str:
        return "StartHackerNewsProfile"

    def execute(self, context: dex.Context, input: ProfileRequest) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ReadHackerNewsProfile"), input)


# dex:group group-id:profile group-label:"Public profile"
# dex:explanation text:"Return the public profile and its canonical permalink."
class ReturnProfile(dex.Step[QueryResult[User]]):
    def get_step_type(self) -> str:
        return "ReturnHackerNewsProfile"

    def execute(self, context: dex.Context, input: QueryResult[User]) -> dex.StepDecision:
        return dex.graceful_complete(input.value)


class ProfileFlow(dex.Flow[ProfileRequest]):
    """Read a Hacker News profile using the Worker's static public connection."""

    def __init__(self, connection: Connection) -> None:
        self._start = StartProfile()
        self._complete = ReturnProfile()
        self._read = new_get_user_step(
            step_type="ReadHackerNewsProfile",
            step_input_type=ProfileRequest,
            annotations={
                "groupId": "profile",
                "groupLabel": "Public profile",
                "explanation": "Read this public user's profile and submitted item IDs.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=profile_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        return "HackerNewsProfilePython"

    def get_steps(self) -> dex.StepList[ProfileRequest]:
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)
