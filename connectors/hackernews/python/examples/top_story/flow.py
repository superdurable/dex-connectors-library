# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Hacker News Flow: read a feed's first story ID, then that story."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.hackernews import (
    Connection,
    GetItemInput,
    Item,
    ListStoryIDsInput,
    StoryIDs,
    new_get_item_step,
    new_list_story_ids_step,
)

FLOW_TYPE = "HackerNewsTopStory"
CONNECTION_NAME = "hackernews"
_LIST_STEP_TYPE = "ListFeedStoryIDs"
_GET_STEP_TYPE = "ReadTopStory"
_ANNOTATION_GROUP = {"groupId": "hacker-news", "groupLabel": "Hacker News"}


@dataclass(frozen=True, slots=True)
class TopStoryRequest:
    """Feed to read: top, best, new, ask, or show; blank means top."""

    feed: str = ""


@dataclass(frozen=True, slots=True)
class TopStoryOutcome:
    """The feed's first story, or an empty item when the feed was empty."""

    story: Item = field(default_factory=Item)


_OUTCOME = dex.Attribute("hackernews-top-story-outcome", TopStoryOutcome)


def map_to_list_story_ids_input(request: TopStoryRequest) -> ListStoryIDsInput:
    """Read only the first ID of the requested feed."""
    return ListStoryIDsInput(feed=request.feed, limit=1)


def map_to_get_item_input(item: GetItemInput) -> GetItemInput:
    """Read the story chosen from the feed unchanged."""
    return item


# dex:group group-id:top-story group-label:"Top story"
# dex:explanation text:"Start the lookup with the requested feed."
class _StartRequest(dex.Step[TopStoryRequest]):
    def get_step_type(self) -> str:
        return "StartTopStory"

    def execute(self, context: dex.Context, input: TopStoryRequest) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_LIST_STEP_TYPE), input)


# dex:group group-id:top-story group-label:"Top story"
# dex:explanation text:"Complete when the feed is empty, otherwise read its first story."
class _AfterFeed(dex.Step[QueryResult[StoryIDs]]):
    def get_step_type(self) -> str:
        return "AfterFeedSnapshot"

    def execute(self, context: dex.Context, input: QueryResult[StoryIDs]) -> dex.StepDecision:
        if not input.value.ids:
            outcome = TopStoryOutcome()
            _OUTCOME.set(context, outcome)
            return dex.graceful_complete(outcome)
        return dex.go_to(dex.StepRef(_GET_STEP_TYPE), GetItemInput(input.value.ids[0]))


# dex:group group-id:top-story group-label:"Top story"
# dex:explanation text:"Record the story and complete the Flow."
class _CompleteStory(dex.Step[QueryResult[Item]]):
    def get_step_type(self) -> str:
        return "CompleteTopStory"

    def execute(self, context: dex.Context, input: QueryResult[Item]) -> dex.StepDecision:
        outcome = TopStoryOutcome(input.value)
        _OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class TopStoryFlow(dex.Flow[TopStoryRequest]):
    """Read a feed's first story through the static hackernews connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("top story requires the static hackernews connection")
        self._start = _StartRequest()
        self._after_feed = _AfterFeed()
        self._complete = _CompleteStory()
        self._list = new_list_story_ids_step(
            step_type=_LIST_STEP_TYPE,
            step_input_type=TopStoryRequest,
            annotations={
                **_ANNOTATION_GROUP,
                "explanation": "Read the first story ID of the requested feed.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_list_story_ids_input,
            listed=self._after_feed,
        )
        self._get = new_get_item_step(
            step_type=_GET_STEP_TYPE,
            step_input_type=GetItemInput,
            annotations={**_ANNOTATION_GROUP, "explanation": "Read the story and its discussion link."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_item_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[TopStoryRequest]:
        """Register the start, both Hacker News reads, and their result Steps."""
        return dex.StepList.start_step(self._start).other_steps(
            self._list, self._after_feed, self._get, self._complete
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the story outcome."""
        return dex.PersistenceSchema.of(_OUTCOME)
