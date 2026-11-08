# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Hootsuite social profile listing Flow using the listSocialProfiles Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.hootsuite import (
    Connection,
    ListSocialProfilesInput,
    SocialProfilesPage,
    new_list_social_profiles_step,
)

FLOW_TYPE = "HootsuiteSocialProfilesPython"
CONNECTION_NAME = "hootsuite"
_CALL_STEP_TYPE = "CallListSocialProfiles"
_RESULT = dex.Attribute("hootsuite-python-social-profiles", SocialProfilesPage)


def map_to_operation_input(request: ListSocialProfilesInput) -> ListSocialProfilesInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:hootsuite group-label:"Hootsuite"
# dex:explanation text:"Start the Hootsuite request."
class _StartRequest(dex.Step[ListSocialProfilesInput]):
    def get_step_type(self) -> str:
        return "StartListSocialProfiles"

    def execute(self, context: dex.Context, input: ListSocialProfilesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListSocialProfiles"), input)


# dex:group group-id:hootsuite group-label:"Hootsuite"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[SocialProfilesPage]]):
    def get_step_type(self) -> str:
        return "CompleteListSocialProfiles"

    def execute(self, context: dex.Context, input: QueryResult[SocialProfilesPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SocialProfilesFlow(dex.Flow[ListSocialProfilesInput]):
    """Run one listSocialProfiles read through the static hootsuite connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the social profile listing Flow requires the static hootsuite connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_social_profiles_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListSocialProfilesInput,
            annotations={
                "groupId": "hootsuite",
                "groupLabel": "Hootsuite",
                "explanation": "List visible Hootsuite social profile IDs and names.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "HootsuiteSocialProfilesPython"

    def get_steps(self) -> dex.StepList[ListSocialProfilesInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
