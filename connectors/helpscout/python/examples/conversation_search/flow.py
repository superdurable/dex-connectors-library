# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Search one page of Help Scout conversations in a Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.helpscout import Connection, SearchConversationsInput, SearchConversationsOutput
from dex_connectors.helpscout.steps import new_search_conversations_step

CONNECTION_NAME = "helpscout"
FLOW_TYPE = "HelpScoutConversationSearch"
_RESULT = dex.Attribute("helpscout-conversation-search-result", SearchConversationsOutput)


# dex:group group-id:conversation-search group-label:"Conversation search"
# dex:explanation text:"Pass the requested conversation filters into the Help Scout search operation."
class _StartSearch(dex.Step[SearchConversationsInput]):
    def get_step_type(self) -> str:
        """Return the stable starter Step type."""
        return "StartConversationSearch"

    def execute(self, context: dex.Context, input: SearchConversationsInput) -> dex.StepDecision:
        """Route the caller's filters into the provider Query Step."""
        return dex.go_to(dex.StepRef("SearchHelpScoutConversations"), input)


# dex:group group-id:conversation-search group-label:"Conversation search"
# dex:explanation text:"Store the returned conversation page and complete the Flow."
class _SearchComplete(dex.Step[QueryResult[SearchConversationsOutput]]):
    def get_step_type(self) -> str:
        """Return the stable completion Step type."""
        return "CompleteConversationSearch"

    def execute(
        self, context: dex.Context, input: QueryResult[SearchConversationsOutput]
    ) -> dex.StepDecision:
        """Persist the provider page before successful completion."""
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ConversationSearchFlow(dex.Flow[SearchConversationsInput]):
    """Run a one-page Help Scout conversation search through one static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("conversation search requires the static helpscout connection")
        self._start = _StartSearch()
        self._complete = _SearchComplete()
        self._search = new_search_conversations_step(
            step_type="SearchHelpScoutConversations",
            step_input_type=SearchConversationsInput,
            annotations={
                "groupId": "helpscout",
                "groupLabel": "Help Scout",
                "explanation": "Search one Help Scout Inbox conversation page with the supplied filters.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: request,
            searched=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity."""
        return "HelpScoutConversationSearch"

    def get_steps(self) -> dex.StepList[SearchConversationsInput]:
        """Register the start, provider, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._search, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the stored page with Flow persistence."""
        return dex.PersistenceSchema.of(_RESULT)
