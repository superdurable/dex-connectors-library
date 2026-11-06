# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Confluence page through a Dex Query Step."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.atlassian_confluence import Connection, GetPageInput, Page, new_get_page_step

CONNECTION_NAME = "confluence-pages"
FLOW_TYPE = "ConfluenceReadPagePython"
PAGE = dex.Attribute("confluence-read-page-result", Page)


@dataclass(frozen=True, slots=True)
class ReadPageRequest:
    """Non-secret page ID and output format supplied at Flow start."""

    page_id: str = field(metadata={"dex_json_name": "pageId"})
    body_format: str = field(default="markdown", metadata={"dex_json_name": "bodyFormat"})


def map_to_get_page_input(request: ReadPageRequest) -> GetPageInput:
    """Map a safe start request to the Confluence read operation."""
    return GetPageInput(page_id=request.page_id, body_format=request.body_format)


# dex:group group-id:confluence-read group-label:"Read Confluence page"
# dex:explanation text:"Validate the page ID before reading it through the connection."
class PrepareReadPage(dex.Step[ReadPageRequest]):
    """Validate the request before a provider call."""

    def get_step_type(self) -> str:
        return "PrepareReadPage"

    def execute(self, context: dex.Context, input: ReadPageRequest) -> dex.StepDecision:
        if not input.page_id.strip().isdigit():
            return dex.force_fail("pageId must be numeric")
        return dex.go_to(dex.StepRef("GetConfluencePage"), input)


# dex:group group-id:confluence-read group-label:"Read Confluence page"
# dex:explanation text:"Persist the page's safe converted body and complete the Flow."
class CompleteReadPage(dex.Step[QueryResult[Page]]):
    """Complete the Flow with the decoded page."""

    def get_step_type(self) -> str:
        return "CompleteReadPage"

    def execute(self, context: dex.Context, input: QueryResult[Page]) -> dex.StepDecision:
        PAGE.set(context, input.value)
        return dex.graceful_complete(input.value)


class ReadPageFlow(dex.Flow[ReadPageRequest]):
    """Read a single page using the static Confluence connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Confluence read example needs its static connection")
        self.prepare = PrepareReadPage()
        self.complete = CompleteReadPage()
        self.read = new_get_page_step(
            step_type="GetConfluencePage",
            step_input_type=ReadPageRequest,
            annotations={
                "groupId": "confluence-read",
                "groupLabel": "Read Confluence page",
                "explanation": "Read one Confluence page as bounded Markdown or plain text.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_page_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        return "ConfluenceReadPagePython"

    def get_steps(self) -> dex.StepList[ReadPageRequest]:
        return dex.StepList.start_step(self.prepare).other_steps(self.read, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        return dex.PersistenceSchema.of(PAGE)
