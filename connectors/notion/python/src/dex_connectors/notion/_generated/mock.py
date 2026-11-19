# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CreatePageInput, CreatePageOutput, DateRange, GetPageInput, GetPageOutput, Page, PageContent, PageProperty, Parent, PropertyValue, QueryDatabaseInput, QueryDatabaseOutput, QueryFilter, QuerySort, SearchInput, SearchMatch, SearchOutput, UpdatePagePropertiesInput, UpdatePagePropertiesOutput
from .definitions import CONNECTOR_ID, CREATED, CREATE_PAGE_DEFINITION, DEFECT, FOUND, GET_PAGE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, NO_MATCH, PROVIDER_REJECTED, QUERIED, QUERY_DATABASE_DEFINITION, SEARCHED, SEARCH_DEFINITION, UNCERTAIN, UPDATED, UPDATE_PAGE_PROPERTIES_DEFINITION

def search_searched(value: SearchOutput, failure: Failure | None = None) -> Case[SearchOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_searched, '__module__', "dex_connectors.notion.notionmock")

def search_no_match(value: SearchOutput, failure: Failure | None = None) -> Case[SearchOutput]:
    return Case.branch_result(NO_MATCH, value, failure)

setattr(search_no_match, '__module__', "dex_connectors.notion.notionmock")

def search_provider_rejected(value: SearchOutput, failure: Failure | None = None) -> Case[SearchOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_provider_rejected, '__module__', "dex_connectors.notion.notionmock")

def search_invalid_response(value: SearchOutput, failure: Failure | None = None) -> Case[SearchOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_invalid_response, '__module__', "dex_connectors.notion.notionmock")

def search_defect(value: SearchOutput, failure: Failure | None = None) -> Case[SearchOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_defect, '__module__', "dex_connectors.notion.notionmock")

def search_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_retry, '__module__', "dex_connectors.notion.notionmock")

def search_submissions_search() -> Case[SearchOutput]:
    return Case.paged(SEARCHED, (SearchOutput(matches=(SearchMatch(object_type='data_source', id='2a4d6f80-1b3c-4d5e-8f70-000000000002', title='Form submissions (2025)', parent=Parent(type='database_id', id='2a4d6f80-1b3c-4d5e-8f70-000000000012')), SearchMatch(object_type='data_source', id='2a4d6f80-1b3c-4d5e-8f70-000000000001', title='Form submissions', parent=Parent(type='database_id', id='2a4d6f80-1b3c-4d5e-8f70-000000000011'), in_trash=True)), next_cursor='search-page-2', has_more=True), SearchOutput(matches=(SearchMatch(object_type='data_source', id='2a4d6f80-1b3c-4d5e-8f70-112233445566', title='Form submissions', parent=Parent(type='database_id', id='2a4d6f80-1b3c-4d5e-8f70-112233445500')),), next_cursor='search-page-3', has_more=True), SearchOutput(matches=(SearchMatch(object_type='data_source', id='2a4d6f80-1b3c-4d5e-8f70-112233445566', title='Form submissions', parent=Parent(type='database_id', id='2a4d6f80-1b3c-4d5e-8f70-112233445500')), SearchMatch(object_type='data_source', id='2a4d6f80-1b3c-4d5e-8f70-000000000013', title='Form submissions archive', parent=Parent(type='database_id', id='2a4d6f80-1b3c-4d5e-8f70-000000000014'))))))

setattr(search_submissions_search, '__module__', "dex_connectors.notion.notionmock")

def search_no_shared_title() -> Case[SearchOutput]:
    return Case.branch_result(NO_MATCH, SearchOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "search", "No shared Notion title matches the search."))

setattr(search_no_shared_title, '__module__', "dex_connectors.notion.notionmock")

def search_rejected() -> Case[SearchOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "search", "Notion rejected the search."))

setattr(search_rejected, '__module__', "dex_connectors.notion.notionmock")

def query_database_queried(value: QueryDatabaseOutput, failure: Failure | None = None) -> Case[QueryDatabaseOutput]:
    return Case.branch_result(QUERIED, value, failure)

setattr(query_database_queried, '__module__', "dex_connectors.notion.notionmock")

def query_database_not_found(value: QueryDatabaseOutput, failure: Failure | None = None) -> Case[QueryDatabaseOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(query_database_not_found, '__module__', "dex_connectors.notion.notionmock")

def query_database_provider_rejected(value: QueryDatabaseOutput, failure: Failure | None = None) -> Case[QueryDatabaseOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_database_provider_rejected, '__module__', "dex_connectors.notion.notionmock")

def query_database_invalid_response(value: QueryDatabaseOutput, failure: Failure | None = None) -> Case[QueryDatabaseOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_database_invalid_response, '__module__', "dex_connectors.notion.notionmock")

def query_database_defect(value: QueryDatabaseOutput, failure: Failure | None = None) -> Case[QueryDatabaseOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_database_defect, '__module__', "dex_connectors.notion.notionmock")

def query_database_retry(failure: Failure, after_seconds: float = 0) -> Case[QueryDatabaseOutput]:
    return Case.retry(failure, after_seconds)

setattr(query_database_retry, '__module__', "dex_connectors.notion.notionmock")

def query_database_existing_submission() -> Case[QueryDatabaseOutput]:
    return Case.paged(QUERIED, (QueryDatabaseOutput(data_source_id='2a4d6f80-1b3c-4d5e-8f70-112233445566', pages=(Page(id='2a4d6f80-1b3c-4d5e-8f70-112233445588', title='Ada Lovelace', parent=Parent(type='data_source_id', id='2a4d6f80-1b3c-4d5e-8f70-112233445566', database_id='2a4d6f80-1b3c-4d5e-8f70-112233445500'), in_trash=True, properties={'Submission ID': PageProperty(id='submission-id', type='rich_text', text='sub-new-1', plain_text='sub-new-1')}), Page(id='2a4d6f80-1b3c-4d5e-8f70-112233445599', title='Ada Lovelace', parent=Parent(type='data_source_id', id='2a4d6f80-1b3c-4d5e-8f70-112233445566', database_id='2a4d6f80-1b3c-4d5e-8f70-112233445500'), archived=True, properties={'Submission ID': PageProperty(id='submission-id', type='rich_text', text='sub-new-1', plain_text='sub-new-1')})), next_cursor='rows-page-2', has_more=True), QueryDatabaseOutput(data_source_id='2a4d6f80-1b3c-4d5e-8f70-112233445566', pages=(Page(id='2a4d6f80-1b3c-4d5e-8f70-112233445577', url='https://www.notion.so/2a4d6f801b3c4d5e8f70112233445577', title='Ada Lovelace', parent=Parent(type='data_source_id', id='2a4d6f80-1b3c-4d5e-8f70-112233445566', database_id='2a4d6f80-1b3c-4d5e-8f70-112233445500'), properties={'Name': PageProperty(id='title', type='title', text='Ada Lovelace', plain_text='Ada Lovelace'), 'Submission ID': PageProperty(id='submission-id', type='rich_text', text='sub-new-1', plain_text='sub-new-1'), 'Email': PageProperty(id='email', type='email', email='ada@example.com', plain_text='ada@example.com'), 'Submitted at': PageProperty(id='submitted-at', type='date', date=DateRange(start='2026-10-25T00:00:00Z'), plain_text='2026-10-25T00:00:00Z')}),), next_cursor='rows-page-3', has_more=True), QueryDatabaseOutput(data_source_id='2a4d6f80-1b3c-4d5e-8f70-112233445566', pages=(Page(id='2a4d6f80-1b3c-4d5e-8f70-112233445577', url='https://www.notion.so/2a4d6f801b3c4d5e8f70112233445577', title='Ada Lovelace', parent=Parent(type='data_source_id', id='2a4d6f80-1b3c-4d5e-8f70-112233445566', database_id='2a4d6f80-1b3c-4d5e-8f70-112233445500'), properties={'Name': PageProperty(id='title', type='title', text='Ada Lovelace', plain_text='Ada Lovelace'), 'Submission ID': PageProperty(id='submission-id', type='rich_text', text='sub-new-1', plain_text='sub-new-1'), 'Email': PageProperty(id='email', type='email', email='ada@example.com', plain_text='ada@example.com'), 'Submitted at': PageProperty(id='submitted-at', type='date', date=DateRange(start='2026-10-25T00:00:00Z'), plain_text='2026-10-25T00:00:00Z')}),))))

setattr(query_database_existing_submission, '__module__', "dex_connectors.notion.notionmock")

def query_database_data_source_missing() -> Case[QueryDatabaseOutput]:
    return Case.branch_result(NOT_FOUND, QueryDatabaseOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "queryDatabase", "Notion could not find the data source."))

setattr(query_database_data_source_missing, '__module__', "dex_connectors.notion.notionmock")

def query_database_query_rejected() -> Case[QueryDatabaseOutput]:
    return Case.branch_result(PROVIDER_REJECTED, QueryDatabaseOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "queryDatabase", "Notion rejected the database filter."))

setattr(query_database_query_rejected, '__module__', "dex_connectors.notion.notionmock")

def get_page_found(value: GetPageOutput, failure: Failure | None = None) -> Case[GetPageOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_page_found, '__module__', "dex_connectors.notion.notionmock")

def get_page_not_found(value: GetPageOutput, failure: Failure | None = None) -> Case[GetPageOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_page_not_found, '__module__', "dex_connectors.notion.notionmock")

def get_page_provider_rejected(value: GetPageOutput, failure: Failure | None = None) -> Case[GetPageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_page_provider_rejected, '__module__', "dex_connectors.notion.notionmock")

def get_page_invalid_response(value: GetPageOutput, failure: Failure | None = None) -> Case[GetPageOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_page_invalid_response, '__module__', "dex_connectors.notion.notionmock")

def get_page_defect(value: GetPageOutput, failure: Failure | None = None) -> Case[GetPageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_page_defect, '__module__', "dex_connectors.notion.notionmock")

def get_page_retry(failure: Failure, after_seconds: float = 0) -> Case[GetPageOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_page_retry, '__module__', "dex_connectors.notion.notionmock")

def get_page_submitted_page() -> Case[GetPageOutput]:
    return Case.branch_result(FOUND, GetPageOutput(page=Page(id='2a4d6f80-1b3c-4d5e-8f70-112233445577', url='https://www.notion.so/2a4d6f801b3c4d5e8f70112233445577', title='Ada Lovelace', parent=Parent(type='data_source_id', id='2a4d6f80-1b3c-4d5e-8f70-112233445566', database_id='2a4d6f80-1b3c-4d5e-8f70-112233445500'), properties={'Name': PageProperty(id='title', type='title', text='Ada Lovelace', plain_text='Ada Lovelace'), 'Submission ID': PageProperty(id='submission-id', type='rich_text', text='sub-new-1', plain_text='sub-new-1'), 'Email': PageProperty(id='email', type='email', email='ada@example.com', plain_text='ada@example.com'), 'Submitted at': PageProperty(id='submitted-at', type='date', date=DateRange(start='2026-10-25T00:00:00Z'), plain_text='2026-10-25T00:00:00Z')}), content=PageContent(text='Hello from the form.\nSecond paragraph.', block_count=2)))

setattr(get_page_submitted_page, '__module__', "dex_connectors.notion.notionmock")

def get_page_page_missing() -> Case[GetPageOutput]:
    return Case.branch_result(NOT_FOUND, GetPageOutput(page=Page(parent=Parent(type='')), content=PageContent()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getPage", "Notion could not find the page."))

setattr(get_page_page_missing, '__module__', "dex_connectors.notion.notionmock")

def get_page_page_read_rejected() -> Case[GetPageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, GetPageOutput(page=Page(parent=Parent(type='')), content=PageContent()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getPage", "Notion rejected the page read."))

setattr(get_page_page_read_rejected, '__module__', "dex_connectors.notion.notionmock")

def create_page_created(value: CreatePageOutput, failure: Failure | None = None) -> Case[CreatePageOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_page_created, '__module__', "dex_connectors.notion.notionmock")

def create_page_not_found(value: CreatePageOutput, failure: Failure | None = None) -> Case[CreatePageOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(create_page_not_found, '__module__', "dex_connectors.notion.notionmock")

def create_page_provider_rejected(value: CreatePageOutput, failure: Failure | None = None) -> Case[CreatePageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_page_provider_rejected, '__module__', "dex_connectors.notion.notionmock")

def create_page_uncertain(value: CreatePageOutput, failure: Failure) -> Case[CreatePageOutput]:
    return Case.uncertain(value, failure)

setattr(create_page_uncertain, '__module__', "dex_connectors.notion.notionmock")

def create_page_invalid_response(value: CreatePageOutput, failure: Failure | None = None) -> Case[CreatePageOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_page_invalid_response, '__module__', "dex_connectors.notion.notionmock")

def create_page_defect(value: CreatePageOutput, failure: Failure | None = None) -> Case[CreatePageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_page_defect, '__module__', "dex_connectors.notion.notionmock")

def create_page_retry(failure: Failure, after_seconds: float = 0) -> Case[CreatePageOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_page_retry, '__module__', "dex_connectors.notion.notionmock")

def create_page_created_submission() -> Case[CreatePageOutput]:
    return Case.branch_result(CREATED, CreatePageOutput(data_source_id='2a4d6f80-1b3c-4d5e-8f70-112233445566', page_id='2a4d6f80-1b3c-4d5e-8f70-112233445577', page=Page(id='2a4d6f80-1b3c-4d5e-8f70-112233445577', url='https://www.notion.so/2a4d6f801b3c4d5e8f70112233445577', title='Ada Lovelace', parent=Parent(type='data_source_id', id='2a4d6f80-1b3c-4d5e-8f70-112233445566', database_id='2a4d6f80-1b3c-4d5e-8f70-112233445500'), properties={'Name': PageProperty(id='title', type='title', text='Ada Lovelace', plain_text='Ada Lovelace'), 'Submission ID': PageProperty(id='submission-id', type='rich_text', text='sub-new-1', plain_text='sub-new-1'), 'Email': PageProperty(id='email', type='email', email='ada@example.com', plain_text='ada@example.com'), 'Submitted at': PageProperty(id='submitted-at', type='date', date=DateRange(start='2026-10-25T00:00:00Z'), plain_text='2026-10-25T00:00:00Z')})))

setattr(create_page_created_submission, '__module__', "dex_connectors.notion.notionmock")

def create_page_create_source_missing() -> Case[CreatePageOutput]:
    return Case.branch_result(NOT_FOUND, CreatePageOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "createPage", "Notion could not find the destination data source."))

setattr(create_page_create_source_missing, '__module__', "dex_connectors.notion.notionmock")

def create_page_create_rejected() -> Case[CreatePageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreatePageOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createPage", "Notion rejected the submitted row."))

setattr(create_page_create_rejected, '__module__', "dex_connectors.notion.notionmock")

def update_page_properties_updated(value: UpdatePagePropertiesOutput, failure: Failure | None = None) -> Case[UpdatePagePropertiesOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_page_properties_updated, '__module__', "dex_connectors.notion.notionmock")

def update_page_properties_not_found(value: UpdatePagePropertiesOutput, failure: Failure | None = None) -> Case[UpdatePagePropertiesOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_page_properties_not_found, '__module__', "dex_connectors.notion.notionmock")

def update_page_properties_provider_rejected(value: UpdatePagePropertiesOutput, failure: Failure | None = None) -> Case[UpdatePagePropertiesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_page_properties_provider_rejected, '__module__', "dex_connectors.notion.notionmock")

def update_page_properties_invalid_response(value: UpdatePagePropertiesOutput, failure: Failure | None = None) -> Case[UpdatePagePropertiesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_page_properties_invalid_response, '__module__', "dex_connectors.notion.notionmock")

def update_page_properties_defect(value: UpdatePagePropertiesOutput, failure: Failure | None = None) -> Case[UpdatePagePropertiesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_page_properties_defect, '__module__', "dex_connectors.notion.notionmock")

def update_page_properties_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdatePagePropertiesOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_page_properties_retry, '__module__', "dex_connectors.notion.notionmock")

def update_page_properties_updated_submission() -> Case[UpdatePagePropertiesOutput]:
    return Case.branch_result(UPDATED, UpdatePagePropertiesOutput(page=Page(id='2a4d6f80-1b3c-4d5e-8f70-112233445577', url='https://www.notion.so/2a4d6f801b3c4d5e8f70112233445577', title='Ada Lovelace', parent=Parent(type='data_source_id', id='2a4d6f80-1b3c-4d5e-8f70-112233445566', database_id='2a4d6f80-1b3c-4d5e-8f70-112233445500'), properties={'Name': PageProperty(id='title', type='title', text='Ada Lovelace', plain_text='Ada Lovelace'), 'Submission ID': PageProperty(id='submission-id', type='rich_text', text='sub-new-1', plain_text='sub-new-1'), 'Email': PageProperty(id='email', type='email', email='ada@example.com', plain_text='ada@example.com'), 'Submitted at': PageProperty(id='submitted-at', type='date', date=DateRange(start='2026-10-25T00:00:00Z'), plain_text='2026-10-25T00:00:00Z')})))

setattr(update_page_properties_updated_submission, '__module__', "dex_connectors.notion.notionmock")

def update_page_properties_update_page_missing() -> Case[UpdatePagePropertiesOutput]:
    return Case.branch_result(NOT_FOUND, UpdatePagePropertiesOutput(page=Page(parent=Parent(type=''))), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updatePageProperties", "Notion could not find the row to update."))

setattr(update_page_properties_update_page_missing, '__module__', "dex_connectors.notion.notionmock")

def update_page_properties_update_rejected() -> Case[UpdatePagePropertiesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdatePagePropertiesOutput(page=Page(parent=Parent(type=''))), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "updatePageProperties", "Notion rejected the row update."))

setattr(update_page_properties_update_rejected, '__module__', "dex_connectors.notion.notionmock")


class Mock:

    def __init__(self, connection_name: str = "notion") -> None:
        self.search_script: Query[SearchInput, SearchOutput] = Query(SEARCH_DEFINITION)
        self.search_script.default(search_submissions_search())
        self.query_database_script: Query[QueryDatabaseInput, QueryDatabaseOutput] = Query(QUERY_DATABASE_DEFINITION)
        self.query_database_script.default(query_database_existing_submission())
        self.get_page_script: Query[GetPageInput, GetPageOutput] = Query(GET_PAGE_DEFINITION)
        self.get_page_script.default(get_page_submitted_page())
        self.create_page_script: Mutation[CreatePageInput, CreatePageOutput] = Mutation(CREATE_PAGE_DEFINITION)
        self.create_page_script.default(create_page_created_submission())
        self.update_page_properties_script: Mutation[UpdatePagePropertiesInput, UpdatePagePropertiesOutput] = Mutation(UPDATE_PAGE_PROPERTIES_DEFINITION)
        self.update_page_properties_script.default(update_page_properties_updated_submission())
        self._connection = new_connection_with_operations(
            search=self.search_script,
            query_database=self.query_database_script,
            get_page=self.get_page_script,
            create_page=self.create_page_script,
            update_page_properties=self.update_page_properties_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.notion.notionmock")

__all__ = [
    "Mock",
    "search_searched",
    "search_no_match",
    "search_provider_rejected",
    "search_invalid_response",
    "search_defect",
    "search_retry",
    "search_submissions_search",
    "search_no_shared_title",
    "search_rejected",
    "query_database_queried",
    "query_database_not_found",
    "query_database_provider_rejected",
    "query_database_invalid_response",
    "query_database_defect",
    "query_database_retry",
    "query_database_existing_submission",
    "query_database_data_source_missing",
    "query_database_query_rejected",
    "get_page_found",
    "get_page_not_found",
    "get_page_provider_rejected",
    "get_page_invalid_response",
    "get_page_defect",
    "get_page_retry",
    "get_page_submitted_page",
    "get_page_page_missing",
    "get_page_page_read_rejected",
    "create_page_created",
    "create_page_not_found",
    "create_page_provider_rejected",
    "create_page_uncertain",
    "create_page_invalid_response",
    "create_page_defect",
    "create_page_retry",
    "create_page_created_submission",
    "create_page_create_source_missing",
    "create_page_create_rejected",
    "update_page_properties_updated",
    "update_page_properties_not_found",
    "update_page_properties_provider_rejected",
    "update_page_properties_invalid_response",
    "update_page_properties_defect",
    "update_page_properties_retry",
    "update_page_properties_updated_submission",
    "update_page_properties_update_page_missing",
    "update_page_properties_update_rejected",
]
