# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from .timestamps import parse_go_datetime, validate_go_datetime

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# IssueSearchFilter is a typed search that the connector turns into escaped JQL, so no caller text can
# change the query's structure. Every set field adds one clause, and clauses are joined with AND.
# At least one clause is required because Jira accepts only bounded queries.
@dataclass(frozen=True, slots=True)
class IssueSearchFilter:
    # ProjectKeys limits the search to these project keys, such as OPS.
    project_keys: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'projectKeys', 'dex_json_omit_default': True})
    # IssueTypeNames limits the search to these issue type names, such as Task or Bug.
    issue_type_names: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'issueTypeNames', 'dex_json_omit_default': True})
    # StatusNames limits the search to these workflow status names, such as In Review.
    status_names: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'statusNames', 'dex_json_omit_default': True})
    # StatusCategories limits the search to these status categories: new, indeterminate, or done.
    status_categories: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'statusCategories', 'dex_json_omit_default': True})
    # AssigneeAccountIDs limits the search to issues assigned to these Atlassian account IDs.
    assignee_account_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'assigneeAccountIds', 'dex_json_omit_default': True})
    # IncludesUnassigned also matches unassigned issues; alone it matches only unassigned issues.
    includes_unassigned: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includesUnassigned', 'dex_json_omit_default': True})
    # Labels matches issues that carry at least one of these labels.
    labels: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # SummaryPhrase matches issues whose summary contains this phrase as Jira's text search tokenizes it.
    # Text search ignores case and punctuation, so compare the returned summaries for an exact match.
    summary_phrase: str = _dataclass_field(default='', metadata={'dex_json_name': 'summaryPhrase', 'dex_json_omit_default': True})
    # Order selects the result order; blank uses IssueSearchOrderCreatedDescending.
    order: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(IssueSearchFilter, '__module__', 'dex_connectors.atlassian_jira.models')


# SearchIssuesInput selects one page of issues. Set exactly one of Filter and JQL.
@dataclass(frozen=True, slots=True)
class SearchIssuesInput:
    # Filter is a typed search the connector escapes into bounded JQL. Prefer it to JQL.
    filter: IssueSearchFilter | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # JQL is a caller-built bounded query, such as project = "OPS" ORDER BY created DESC. Quote every
    # value that came from outside the application with QuoteJQLString.
    jql: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # AdditionalFields lists up to 20 more field IDs to return raw, such as customfield_10020 or duedate.
    additional_fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'additionalFields', 'dex_json_omit_default': True})
    # PageSize is the number of issues to request, 1 to 100; zero requests 50. Jira may return fewer.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # NextPageToken continues a previous search with the same query; blank reads the first page.
    # Jira expires a token after seven days.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})


setattr(SearchIssuesInput, '__module__', 'dex_connectors.atlassian_jira.models')


# IssueStatus is one Jira workflow status.
@dataclass(frozen=True, slots=True)
class IssueStatus:
    # ID is the status ID, stable across renames.
    id: str = ''
    # Name is the site's status name, such as In Review.
    name: str = ''
    # CategoryKey is the status category: new, indeterminate, or done.
    category_key: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'categoryKey', 'dex_json_omit_default': True})


setattr(IssueStatus, '__module__', 'dex_connectors.atlassian_jira.models')


# IssueTypeReference identifies an issue type.
@dataclass(frozen=True, slots=True)
class IssueTypeReference:
    # ID is the issue type ID.
    id: str = ''
    # Name is the issue type name, such as Task.
    name: str = ''
    # IsSubtask reports a sub-task issue type.
    is_subtask: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSubtask', 'dex_json_omit_default': True})


setattr(IssueTypeReference, '__module__', 'dex_connectors.atlassian_jira.models')


# ProjectReference identifies a Jira project.
@dataclass(frozen=True, slots=True)
class ProjectReference:
    # ID is the numeric project ID.
    id: str = ''
    # Key is the project key, such as OPS.
    key: str = ''
    # Name is the project display name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ProjectReference, '__module__', 'dex_connectors.atlassian_jira.models')


# AccountReference identifies an Atlassian account without its email address.
@dataclass(frozen=True, slots=True)
class AccountReference:
    # AccountID is the Atlassian account ID used for assignment.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    # DisplayName is the account's public display name.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})


setattr(AccountReference, '__module__', 'dex_connectors.atlassian_jira.models')


# PriorityReference identifies an issue priority.
@dataclass(frozen=True, slots=True)
class PriorityReference:
    # ID is the priority ID.
    id: str = ''
    # Name is the priority name, such as High.
    name: str = ''


setattr(PriorityReference, '__module__', 'dex_connectors.atlassian_jira.models')


# Issue is the connector's view of one Jira issue. Status, issue type, and priority names are the
# site's own values, never mapped to another vocabulary.
@dataclass(frozen=True, slots=True)
class Issue:
    # ID is Jira's numeric issue ID, stable when the issue moves between projects.
    id: str = ''
    # Key is the human-readable issue key, such as OPS-441; it changes when the issue moves.
    key: str = ''
    # Summary is the one-line issue title.
    summary: str = ''
    # Status is the issue's current workflow status.
    status: IssueStatus = _dataclass_field(default_factory=IssueStatus)
    # IssueType is the issue's type, such as Task or Bug.
    issue_type: IssueTypeReference = _dataclass_field(default_factory=IssueTypeReference, metadata={'dex_json_name': 'issueType'})
    # Project is the project that holds the issue.
    project: ProjectReference = _dataclass_field(default_factory=ProjectReference)
    # Assignee is the assigned account, or nil for an unassigned issue.
    assignee: AccountReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Reporter is the reporting account, or nil when Jira hides it.
    reporter: AccountReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Priority is the issue priority, or nil when the project does not use priorities.
    priority: PriorityReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Labels lists the issue's labels.
    labels: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Description is the plain text of the description, returned by getIssue only.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsDescriptionTruncated reports that Description stopped at 32767 characters.
    is_description_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDescriptionTruncated', 'dex_json_omit_default': True})
    # CreatedAt is when the issue was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the issue last changed.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_format': 'rfc3339'})
    # AdditionalFields holds the raw Jira JSON of each requested additional field that the issue has,
    # keyed by field ID such as customfield_10020. A requested field Jira omits is absent.
    additional_fields: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'additionalFields', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime(self.updated_at, 'updated_at'))
        elif self.updated_at is not None:
            validate_go_datetime(self.updated_at, 'updated_at')


setattr(Issue, '__module__', 'dex_connectors.atlassian_jira.models')


# SearchIssuesOutput is one page of matching issues.
@dataclass(frozen=True, slots=True)
class SearchIssuesOutput:
    # JQL is the exact query sent to Jira.
    jql: str = ''
    # Issues lists the page's issues with their standard fields and requested additional fields.
    issues: tuple[Issue, ...] = ()
    # NextPageToken continues the search; it is empty on the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'issues', _replace_nil_slices(self.issues, 1))


SearchIssuesOutput.__annotations__['issues'] = tuple[Issue, ...] | None
SearchIssuesOutput.__dataclass_fields__['issues'].type = tuple[Issue, ...] | None
setattr(SearchIssuesOutput, '__module__', 'dex_connectors.atlassian_jira.models')


# GetIssueInput identifies one issue.
@dataclass(frozen=True, slots=True)
class GetIssueInput:
    # IssueIDOrKey is an issue key such as OPS-441 or a numeric issue ID. Jira also finds a moved
    # issue by its old key and returns its current key.
    issue_id_or_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueIdOrKey'})
    # AdditionalFields lists up to 20 more field IDs to return raw, such as customfield_10020 or duedate.
    additional_fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'additionalFields', 'dex_json_omit_default': True})


setattr(GetIssueInput, '__module__', 'dex_connectors.atlassian_jira.models')


# CreateIssueInput describes one issue. Set exactly one of ProjectKey and ProjectID, and exactly one of
# IssueTypeName and IssueTypeID.
@dataclass(frozen=True, slots=True)
class CreateIssueInput:
    # ProjectKey is the project key, such as OPS.
    project_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectKey', 'dex_json_omit_default': True})
    # ProjectID is the numeric project ID, as the project picker stores it.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId', 'dex_json_omit_default': True})
    # IssueTypeName is an issue type available in the project, such as Task or Bug.
    issue_type_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueTypeName', 'dex_json_omit_default': True})
    # IssueTypeID is the numeric issue type ID.
    issue_type_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueTypeId', 'dex_json_omit_default': True})
    # Summary is the one-line title, at most 255 characters.
    summary: str = ''
    # Description is plain text of at most 32767 characters. Blank lines separate paragraphs and other
    # line breaks are kept; the text is never read as markup. Blank sends no description.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Labels lists labels without whitespace, such as incident or customer-reported.
    labels: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # AssigneeAccountID assigns the issue to this Atlassian account ID; blank leaves the project default.
    assignee_account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeAccountId', 'dex_json_omit_default': True})


setattr(CreateIssueInput, '__module__', 'dex_connectors.atlassian_jira.models')


# CreateIssueOutput identifies the created issue. On providerRejected and uncertain, IssueID and IssueKey
# are empty and the requested project and summary are echoed so the application can reconcile.
@dataclass(frozen=True, slots=True)
class CreateIssueOutput:
    # IssueID is the new issue's numeric ID.
    issue_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueId', 'dex_json_omit_default': True})
    # IssueKey is the new issue's key, such as OPS-442.
    issue_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueKey', 'dex_json_omit_default': True})
    # ProjectKey echoes the requested project key.
    project_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectKey', 'dex_json_omit_default': True})
    # ProjectID echoes the requested project ID.
    project_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectId', 'dex_json_omit_default': True})
    # Summary echoes the requested summary.
    summary: str = ''
    # RejectedFieldIDs lists the field IDs Jira named in a rejection, such as summary or issuetype.
    rejected_field_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'rejectedFieldIds', 'dex_json_omit_default': True})


setattr(CreateIssueOutput, '__module__', 'dex_connectors.atlassian_jira.models')


# TransitionIssueInput selects one workflow transition. Set at least one of TransitionID, TransitionName,
# and DestinationStatusName; every one that is set must match the same available transition.
@dataclass(frozen=True, slots=True)
class TransitionIssueInput:
    # IssueIDOrKey is an issue key such as OPS-441 or a numeric issue ID.
    issue_id_or_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueIdOrKey'})
    # TransitionID is the workflow transition ID, such as 31.
    transition_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'transitionId', 'dex_json_omit_default': True})
    # TransitionName is the transition's name as Jira shows it on the issue, such as Start Progress,
    # compared without case.
    transition_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'transitionName', 'dex_json_omit_default': True})
    # DestinationStatusName is the status the transition leads to, such as In Progress, compared without
    # case. When the issue is already in it, no transition is performed. Set it whenever the destination
    # is known: a retried Step then recognizes a transition Jira already made.
    destination_status_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'destinationStatusName', 'dex_json_omit_default': True})
    # ResolutionName sets the resolution, such as Done, for a transition whose screen requires one;
    # blank sends no fields. Jira rejects it for a transition without a resolution field.
    resolution_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'resolutionName', 'dex_json_omit_default': True})


setattr(TransitionIssueInput, '__module__', 'dex_connectors.atlassian_jira.models')


# TransitionReference is one workflow transition available on an issue.
@dataclass(frozen=True, slots=True)
class TransitionReference:
    # ID is the transition ID.
    id: str = ''
    # Name is the transition name.
    name: str = ''
    # DestinationStatus is the status the transition leads to.
    destination_status: IssueStatus = _dataclass_field(default_factory=IssueStatus, metadata={'dex_json_name': 'destinationStatus'})


setattr(TransitionReference, '__module__', 'dex_connectors.atlassian_jira.models')


# TransitionIssueOutput describes the issue's workflow position after the operation.
@dataclass(frozen=True, slots=True)
class TransitionIssueOutput:
    # IssueID is the issue's numeric ID.
    issue_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueId'})
    # IssueKey is the issue's current key.
    issue_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueKey'})
    # PreviousStatus is the status the operation read before it acted.
    previous_status: IssueStatus = _dataclass_field(default_factory=IssueStatus, metadata={'dex_json_name': 'previousStatus'})
    # Status is the issue's status after the operation.
    status: IssueStatus = _dataclass_field(default_factory=IssueStatus)
    # Transition is the matched transition, or nil when none matched.
    transition: TransitionReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # IsAlreadyInDestinationStatus reports that the issue was already in the destination, so nothing was sent.
    is_already_in_destination_status: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAlreadyInDestinationStatus', 'dex_json_omit_default': True})
    # AvailableTransitions lists up to 50 transitions available from Status, set on transitionUnavailable.
    available_transitions: tuple[TransitionReference, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'availableTransitions', 'dex_json_omit_default': True})
    # RejectedFieldIDs lists the field IDs Jira named in a rejection, such as resolution.
    rejected_field_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'rejectedFieldIds', 'dex_json_omit_default': True})


setattr(TransitionIssueOutput, '__module__', 'dex_connectors.atlassian_jira.models')


# AddCommentInput describes one comment on one issue.
@dataclass(frozen=True, slots=True)
class AddCommentInput:
    # IssueIDOrKey is an issue key such as OPS-441 or a numeric issue ID.
    issue_id_or_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueIdOrKey'})
    # Body is plain text of at most 32767 characters. Blank lines separate paragraphs and other line
    # breaks are kept; the text is never read as markup.
    body: str = ''


setattr(AddCommentInput, '__module__', 'dex_connectors.atlassian_jira.models')


# AddCommentOutput identifies the added comment. On notFound, providerRejected, and uncertain,
# CommentID is empty and IssueIDOrKey echoes the request.
@dataclass(frozen=True, slots=True)
class AddCommentOutput:
    # IssueIDOrKey echoes the requested issue.
    issue_id_or_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueIdOrKey'})
    # CommentID is the new comment's numeric ID.
    comment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'commentId', 'dex_json_omit_default': True})
    # AuthorAccountID is the account that wrote the comment, the authorized user.
    author_account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorAccountId', 'dex_json_omit_default': True})
    # CreatedAt is when Jira stored the comment.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # RejectedFieldIDs lists the field IDs Jira named in a rejection.
    rejected_field_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'rejectedFieldIds', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')


setattr(AddCommentOutput, '__module__', 'dex_connectors.atlassian_jira.models')
