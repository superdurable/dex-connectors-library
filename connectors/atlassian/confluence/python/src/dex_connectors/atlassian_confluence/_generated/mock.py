# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddCommentInput, AddCommentOutput, CreatePageInput, CreatePageOutput, GetPageInput, Page, PageSearchFilter, PageSummary, PageVersion, SearchPagesInput, SearchPagesOutput, UpdatePageInput, UpdatePageOutput
from .definitions import CONNECTOR_ID, PROVIDER, ADDED, ADD_COMMENT_DEFINITION, CREATED, CREATE_PAGE_DEFINITION, DEFECT, FOUND, GET_PAGE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_PAGES_DEFINITION, TITLE_CONFLICT, UNCERTAIN, UPDATED, UPDATE_PAGE_DEFINITION, VERSION_CONFLICT

def search_pages_searched(value: SearchPagesOutput, failure: Failure | None = None) -> Case[SearchPagesOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_pages_searched, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def search_pages_provider_rejected(value: SearchPagesOutput, failure: Failure | None = None) -> Case[SearchPagesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_pages_provider_rejected, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def search_pages_invalid_response(value: SearchPagesOutput, failure: Failure | None = None) -> Case[SearchPagesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_pages_invalid_response, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def search_pages_defect(value: SearchPagesOutput, failure: Failure | None = None) -> Case[SearchPagesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_pages_defect, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def search_pages_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchPagesOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_pages_retry, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def search_pages_related_remote_work_policies() -> Case[SearchPagesOutput]:
    return Case.paged(SEARCHED, (SearchPagesOutput(cql='type = page AND space in ("OPS") AND title ~ "\\"Remote work policy\\"" ORDER BY lastmodified DESC', pages=(PageSummary(id='600001', title='Remote work policy (2019 draft)', status='current', space_id='98306', space_key='OPS', version_number=2, last_modified_at=datetime.fromisoformat("2026-09-01T09:00:00Z".replace('Z', '+00:00'))),), next_cursor='related-page-2'), SearchPagesOutput(cql='type = page AND space in ("OPS") AND title ~ "\\"Remote work policy\\"" ORDER BY lastmodified DESC', pages=(PageSummary(id='600001', title='Remote work policy (2019 draft)', status='current', space_id='98306', space_key='OPS', version_number=2, last_modified_at=datetime.fromisoformat("2026-09-01T09:00:00Z".replace('Z', '+00:00'))),), next_cursor='related-page-3'), SearchPagesOutput(cql='type = page AND space in ("OPS") AND title ~ "\\"Remote work policy\\"" ORDER BY lastmodified DESC', pages=(), next_cursor='')))

setattr(search_pages_related_remote_work_policies, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def search_pages_search_denied() -> Case[SearchPagesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchPagesOutput(cql='type = page AND space in ("OPS") AND title ~ "\\"Remote work policy\\"" ORDER BY lastmodified DESC', pages=()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "searchPages", "Confluence denied the page search."))

setattr(search_pages_search_denied, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def get_page_found(value: Page, failure: Failure | None = None) -> Case[Page]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_page_found, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def get_page_not_found(value: Page, failure: Failure | None = None) -> Case[Page]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_page_not_found, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def get_page_provider_rejected(value: Page, failure: Failure | None = None) -> Case[Page]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_page_provider_rejected, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def get_page_invalid_response(value: Page, failure: Failure | None = None) -> Case[Page]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_page_invalid_response, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def get_page_defect(value: Page, failure: Failure | None = None) -> Case[Page]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_page_defect, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def get_page_retry(failure: Failure, after_seconds: float = 0) -> Case[Page]:
    return Case.retry(failure, after_seconds)

setattr(get_page_retry, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def get_page_published_remote_work_page() -> Case[Page]:
    return Case.branch_result(FOUND, Page(id='600002', title='Remote work policy', status='current', space_id='98306', parent_page_id='65537', created_at=datetime.fromisoformat("2026-10-25T09:00:00Z".replace('Z', '+00:00')), version=PageVersion(number=1, created_at=datetime.fromisoformat("2026-10-25T09:00:00Z".replace('Z', '+00:00'))), body='# Remote work\n\nStaff may work **remotely** two days a week.\n\n- Ask your manager\n- Log the days', body_format='markdown', web_url='https://ops.atlassian.net/wiki/spaces/OPS/pages/600002'))

setattr(get_page_published_remote_work_page, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def get_page_missing_remote_work_page() -> Case[Page]:
    return Case.branch_result(NOT_FOUND, Page(version=PageVersion(), body_format=''), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getPage", "Confluence page was not found."))

setattr(get_page_missing_remote_work_page, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def get_page_page_read_denied() -> Case[Page]:
    return Case.branch_result(PROVIDER_REJECTED, Page(version=PageVersion(), body_format=''), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getPage", "Confluence denied the page read."))

setattr(get_page_page_read_denied, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def create_page_created(value: CreatePageOutput, failure: Failure | None = None) -> Case[CreatePageOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_page_created, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def create_page_title_conflict(value: CreatePageOutput, failure: Failure | None = None) -> Case[CreatePageOutput]:
    return Case.branch_result(TITLE_CONFLICT, value, failure)

setattr(create_page_title_conflict, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def create_page_not_found(value: CreatePageOutput, failure: Failure | None = None) -> Case[CreatePageOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(create_page_not_found, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def create_page_provider_rejected(value: CreatePageOutput, failure: Failure | None = None) -> Case[CreatePageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_page_provider_rejected, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def create_page_defect(value: CreatePageOutput, failure: Failure | None = None) -> Case[CreatePageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_page_defect, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def create_page_retry(failure: Failure, after_seconds: float = 0) -> Case[CreatePageOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_page_retry, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def create_page_created_remote_work_policy() -> Case[CreatePageOutput]:
    return Case.branch_result(CREATED, CreatePageOutput(page_id='600002', title='Remote work policy', space_id='98306', space_key='OPS', parent_page_id='65537', version_number=1, web_url='https://ops.atlassian.net/wiki/spaces/OPS/pages/600002'))

setattr(create_page_created_remote_work_policy, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def create_page_title_taken() -> Case[CreatePageOutput]:
    return Case.branch_result(TITLE_CONFLICT, CreatePageOutput(page_id='600003', title='Remote work policy', space_id='98306', space_key='OPS', parent_page_id='65537', version_number=3, web_url='https://ops.atlassian.net/wiki/spaces/OPS/pages/600003'), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "createPage", "Another page already has this title."))

setattr(create_page_title_taken, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def create_page_space_missing() -> Case[CreatePageOutput]:
    return Case.branch_result(NOT_FOUND, CreatePageOutput(title='Remote work policy', space_key='OPS'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "createPage", "Confluence space was not found."))

setattr(create_page_space_missing, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def create_page_create_denied() -> Case[CreatePageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreatePageOutput(title='Remote work policy', space_id='98306', space_key='OPS'), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createPage", "Confluence denied the page create."))

setattr(create_page_create_denied, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def update_page_updated(value: UpdatePageOutput, failure: Failure | None = None) -> Case[UpdatePageOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_page_updated, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def update_page_version_conflict(value: UpdatePageOutput, failure: Failure | None = None) -> Case[UpdatePageOutput]:
    return Case.branch_result(VERSION_CONFLICT, value, failure)

setattr(update_page_version_conflict, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def update_page_not_found(value: UpdatePageOutput, failure: Failure | None = None) -> Case[UpdatePageOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_page_not_found, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def update_page_provider_rejected(value: UpdatePageOutput, failure: Failure | None = None) -> Case[UpdatePageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_page_provider_rejected, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def update_page_defect(value: UpdatePageOutput, failure: Failure | None = None) -> Case[UpdatePageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_page_defect, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def update_page_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdatePageOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_page_retry, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def update_page_updated_remote_work_policy() -> Case[UpdatePageOutput]:
    return Case.branch_result(UPDATED, UpdatePageOutput(page_id='600002', title='Remote work policy', version_number=2, web_url='https://ops.atlassian.net/wiki/spaces/OPS/pages/600002'))

setattr(update_page_updated_remote_work_policy, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def update_page_competing_edit() -> Case[UpdatePageOutput]:
    return Case.branch_result(VERSION_CONFLICT, UpdatePageOutput(page_id='600002', title='Remote work policy', version_number=2, web_url='https://ops.atlassian.net/wiki/spaces/OPS/pages/600002'), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "updatePage", "Another editor saved the requested version first."))

setattr(update_page_competing_edit, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def update_page_update_missing_page() -> Case[UpdatePageOutput]:
    return Case.branch_result(NOT_FOUND, UpdatePageOutput(page_id='699999', title='Remote work policy'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updatePage", "Confluence page was not found."))

setattr(update_page_update_missing_page, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def update_page_update_denied() -> Case[UpdatePageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdatePageOutput(page_id='600002', title='Remote work policy'), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updatePage", "Confluence denied the page update."))

setattr(update_page_update_denied, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def add_comment_added(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_comment_added, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def add_comment_not_found(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_comment_not_found, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def add_comment_provider_rejected(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_comment_provider_rejected, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def add_comment_uncertain(value: AddCommentOutput, failure: Failure) -> Case[AddCommentOutput]:
    return Case.uncertain(value, failure)

setattr(add_comment_uncertain, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def add_comment_defect(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_comment_defect, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def add_comment_retry(failure: Failure, after_seconds: float = 0) -> Case[AddCommentOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_comment_retry, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def add_comment_publication_comment() -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, AddCommentOutput(page_id='600002', comment_id='700001', created_at=datetime.fromisoformat("2026-10-25T09:01:00Z".replace('Z', '+00:00'))))

setattr(add_comment_publication_comment, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def add_comment_comment_page_missing() -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, AddCommentOutput(page_id='699999'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addComment", "Confluence page was not found."))

setattr(add_comment_comment_page_missing, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

def add_comment_comment_denied() -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddCommentOutput(page_id='600002'), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "addComment", "Confluence denied the comment."))

setattr(add_comment_comment_denied, '__module__', "dex_connectors.atlassian_confluence.confluencemock")


class Mock:

    def __init__(self, connection_name: str = "confluence") -> None:
        self.search_pages_script: Query[SearchPagesInput, SearchPagesOutput] = Query(SEARCH_PAGES_DEFINITION)
        self.search_pages_script.default(search_pages_related_remote_work_policies())
        self.get_page_script: Query[GetPageInput, Page] = Query(GET_PAGE_DEFINITION)
        self.get_page_script.default(get_page_published_remote_work_page())
        self.create_page_script: Mutation[CreatePageInput, CreatePageOutput] = Mutation(CREATE_PAGE_DEFINITION)
        self.create_page_script.default(create_page_created_remote_work_policy())
        self.update_page_script: Mutation[UpdatePageInput, UpdatePageOutput] = Mutation(UPDATE_PAGE_DEFINITION)
        self.update_page_script.default(update_page_updated_remote_work_policy())
        self.add_comment_script: Mutation[AddCommentInput, AddCommentOutput] = Mutation(ADD_COMMENT_DEFINITION)
        self.add_comment_script.default(add_comment_publication_comment())
        self._connection = new_connection_with_operations(
            search_pages=self.search_pages_script,
            get_page=self.get_page_script,
            create_page=self.create_page_script,
            update_page=self.update_page_script,
            add_comment=self.add_comment_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.atlassian_confluence.confluencemock")

__all__ = [
    "Mock",
    "search_pages_searched",
    "search_pages_provider_rejected",
    "search_pages_invalid_response",
    "search_pages_defect",
    "search_pages_retry",
    "search_pages_related_remote_work_policies",
    "search_pages_search_denied",
    "get_page_found",
    "get_page_not_found",
    "get_page_provider_rejected",
    "get_page_invalid_response",
    "get_page_defect",
    "get_page_retry",
    "get_page_published_remote_work_page",
    "get_page_missing_remote_work_page",
    "get_page_page_read_denied",
    "create_page_created",
    "create_page_title_conflict",
    "create_page_not_found",
    "create_page_provider_rejected",
    "create_page_defect",
    "create_page_retry",
    "create_page_created_remote_work_policy",
    "create_page_title_taken",
    "create_page_space_missing",
    "create_page_create_denied",
    "update_page_updated",
    "update_page_version_conflict",
    "update_page_not_found",
    "update_page_provider_rejected",
    "update_page_defect",
    "update_page_retry",
    "update_page_updated_remote_work_policy",
    "update_page_competing_edit",
    "update_page_update_missing_page",
    "update_page_update_denied",
    "add_comment_added",
    "add_comment_not_found",
    "add_comment_provider_rejected",
    "add_comment_uncertain",
    "add_comment_defect",
    "add_comment_retry",
    "add_comment_publication_comment",
    "add_comment_comment_page_missing",
    "add_comment_comment_denied",
]
