# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read kuaishou data through the static Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.kuaishou import Connection, ListUserVideosInput, UserVideosPage, new_list_user_videos_step

CONNECTION_NAME = "kuaishou"
_OUTCOME = dex.Attribute("kuaishou-list_user_videos-result", UserVideosPage)


def map_to_operation_input(input: ListUserVideosInput) -> ListUserVideosInput:
    """Pass the typed request to the provider Query."""
    return input


# dex:group group-id:list-user-videos group-label:"ListUserVideos"
# dex:explanation text:"Start the kuaishou Query."
class _StartRequest(dex.Step[ListUserVideosInput]):
    def get_step_type(self) -> str:
        return "StartListUserVideos"

    def execute(self, context: dex.Context, input: ListUserVideosInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallListUserVideos"), input)


# dex:group group-id:list-user-videos group-label:"ListUserVideos"
# dex:explanation text:"Persist the provider result and complete the Flow."
class _CompleteResult(dex.Step[QueryResult[UserVideosPage]]):
    def get_step_type(self) -> str:
        return "CompleteListUserVideos"

    def execute(self, context: dex.Context, input: QueryResult[UserVideosPage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ListUserVideosFlow(dex.Flow[ListUserVideosInput]):
    """Run listUserVideos using the static `kuaishou` connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("kuaishou example requires the static connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._query = new_list_user_videos_step(
            step_type="CallListUserVideos",
            step_input_type=ListUserVideosInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "kuaishou",
                "explanation": "Call the kuaishou listUserVideos Query.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "KuaishouListUserVideos"

    def get_steps(self) -> dex.StepList[ListUserVideosInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the typed provider output."""
        return dex.PersistenceSchema.of(_OUTCOME)
