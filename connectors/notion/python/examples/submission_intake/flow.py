# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise all five Notion operations in one Dex Flow."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.notion import (
    Connection,
    CreatePageInput,
    CreatePageOutput,
    GetPageInput,
    GetPageOutput,
    PropertyValue,
    QueryDatabaseInput,
    QueryDatabaseOutput,
    SearchInput,
    SearchOutput,
    UpdatePagePropertiesInput,
    UpdatePagePropertiesOutput,
    new_create_page_step,
    new_get_page_step,
    new_query_database_step,
    new_search_step,
    new_update_page_properties_step,
)

CONNECTION_NAME = "notion-submissions"
FLOW_TYPE = "NotionSubmissionIntakePython"


@dataclass(frozen=True, slots=True)
class SubmissionRequest:
    """One submission and the stable Notion IDs chosen at Flow start."""

    data_source_id: str = field(metadata={"dex_json_name": "dataSourceId"})
    page_id: str = field(metadata={"dex_json_name": "pageId"})
    title: str
    email: str


_REQUEST = dex.Attribute("notion-python-submission-request", SubmissionRequest)
_PAGE = dex.Attribute("notion-python-submission-page", GetPageOutput)


def _annotations(explanation: str) -> dict[str, str]:
    return {"groupId": "notion-submission", "groupLabel": "Notion submission", "explanation": explanation}


def _search_input(request: SubmissionRequest) -> SearchInput:
    return SearchInput(query=request.title, object_type="page", page_size=20)


def _query_input(request: SubmissionRequest) -> QueryDatabaseInput:
    return QueryDatabaseInput(data_source_id=request.data_source_id, page_size=20)


def _create_input(request: SubmissionRequest) -> CreatePageInput:
    return CreatePageInput(
        data_source_id=request.data_source_id,
        properties={
            "Name": PropertyValue(type="title", text=request.title),
            "Email": PropertyValue(type="email", email=request.email),
        },
    )


def _update_input(request: SubmissionRequest) -> UpdatePagePropertiesInput:
    return UpdatePagePropertiesInput(
        page_id=request.page_id,
        properties={"Name": PropertyValue(type="title", text=request.title)},
    )


def _get_input(request: SubmissionRequest) -> GetPageInput:
    return GetPageInput(page_id=request.page_id, skip_content=True)


# dex:group group-id:notion-submission group-label:"Notion submission"
# dex:explanation text:"Persist safe submission details before searching Notion."
class PrepareSubmission(dex.Step[SubmissionRequest]):
    """Validate and retain the submitted values."""

    def get_step_type(self) -> str:
        return "PrepareNotionSubmission"

    def execute(self, context: dex.Context, input: SubmissionRequest) -> dex.StepDecision:
        if (
            not input.title.strip()
            or not input.email.strip()
            or not input.data_source_id
            or not input.page_id
        ):
            return dex.force_fail("submission title, email, dataSourceId, and pageId are required")
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef("SearchNotionSubmissions"), input)


# dex:group group-id:notion-submission group-label:"Notion submission"
# dex:explanation text:"Continue from title search to the selected data source."
class AfterSearch(dex.Step[QueryResult[SearchOutput]]):
    """Continue after a successful title search."""

    def get_step_type(self) -> str:
        return "AfterNotionSearch"

    def execute(self, context: dex.Context, input: QueryResult[SearchOutput]) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("QueryNotionDatabase"), _REQUEST.get(context))


# dex:group group-id:notion-submission group-label:"Notion submission"
# dex:explanation text:"Continue from the database query to a fenced page creation."
class AfterQuery(dex.Step[QueryResult[QueryDatabaseOutput]]):
    """Continue after a successful data source query."""

    def get_step_type(self) -> str:
        return "AfterNotionQuery"

    def execute(self, context: dex.Context, input: QueryResult[QueryDatabaseOutput]) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CreateNotionPage"), _REQUEST.get(context))


# dex:group group-id:notion-submission group-label:"Notion submission"
# dex:explanation text:"Continue from confirmed creation to an absolute property update."
class AfterCreate(dex.Step[MutationResult[CreatePageOutput]]):
    """Continue only after the create outcome is definite."""

    def get_step_type(self) -> str:
        return "AfterNotionCreate"

    def execute(self, context: dex.Context, input: MutationResult[CreatePageOutput]) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("UpdateNotionPage"), _REQUEST.get(context))


# dex:group group-id:notion-submission group-label:"Notion submission"
# dex:explanation text:"Read back the page after its absolute property update."
class AfterUpdate(dex.Step[MutationResult[UpdatePagePropertiesOutput]]):
    """Continue to a final page read."""

    def get_step_type(self) -> str:
        return "AfterNotionUpdate"

    def execute(
        self, context: dex.Context, input: MutationResult[UpdatePagePropertiesOutput]
    ) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("GetNotionPage"), _REQUEST.get(context))


# dex:group group-id:notion-submission group-label:"Notion submission"
# dex:explanation text:"Persist the final page and complete the intake Flow."
class CompleteSubmission(dex.Step[QueryResult[GetPageOutput]]):
    """Persist and return the final page read."""

    def get_step_type(self) -> str:
        return "CompleteNotionSubmission"

    def execute(self, context: dex.Context, input: QueryResult[GetPageOutput]) -> dex.StepDecision:
        _PAGE.set(context, input.value)
        return dex.graceful_complete(input.value)


class SubmissionFlow(dex.Flow[SubmissionRequest]):
    """Search, query, create, update, and read through one static connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Notion submission requires its static connection name")
        self.prepare = PrepareSubmission()
        self.after_search = AfterSearch()
        self.after_query = AfterQuery()
        self.after_create = AfterCreate()
        self.after_update = AfterUpdate()
        self.complete = CompleteSubmission()
        self.search = new_search_step(
            step_type="SearchNotionSubmissions",
            step_input_type=SubmissionRequest,
            annotations=_annotations("Search shared Notion page titles."),
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_search_input,
            searched=self.after_search,
        )
        self.query = new_query_database_step(
            step_type="QueryNotionDatabase",
            step_input_type=SubmissionRequest,
            annotations=_annotations("Query one page of database rows."),
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_query_input,
            queried=self.after_query,
        )
        self.create = new_create_page_step(
            step_type="CreateNotionPage",
            step_input_type=SubmissionRequest,
            annotations=_annotations("Create one fenced Notion page."),
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_create_input,
            created=self.after_create,
        )
        self.update = new_update_page_properties_step(
            step_type="UpdateNotionPage",
            step_input_type=SubmissionRequest,
            annotations=_annotations("Replace page properties with absolute values."),
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_update_input,
            updated=self.after_update,
        )
        self.get = new_get_page_step(
            step_type="GetNotionPage",
            step_input_type=SubmissionRequest,
            annotations=_annotations("Read the final Notion page."),
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=_get_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[SubmissionRequest]:
        return dex.StepList.start_step(self.prepare).other_steps(
            self.search,
            self.after_search,
            self.query,
            self.after_query,
            self.create,
            self.after_create,
            self.update,
            self.after_update,
            self.get,
            self.complete,
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        return dex.PersistenceSchema.of(_REQUEST, _PAGE)
