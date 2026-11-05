# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# FindCustomerByEmailInput names a service desk and the email address to look up among its customers.
@dataclass(frozen=True, slots=True)
class FindCustomerByEmailInput:
    # ServiceDeskID is the numeric service desk ID, as the service desk picker stores it.
    service_desk_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'serviceDeskId'})
    # Email is one bare address, such as jane@example.com, compared without case.
    email: str = ''


setattr(FindCustomerByEmailInput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# Customer is one Jira Service Management customer without its email address.
@dataclass(frozen=True, slots=True)
class Customer:
    # AccountID is the customer's Atlassian account ID, which createTicket accepts as RaiseOnBehalfOfAccountID.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    # DisplayName is the customer's display name, which privacy settings may replace.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})
    # IsActive reports an active account; Atlassian omits the flag for some accounts, which reads as true.
    is_active: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isActive'})


setattr(Customer, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# CustomerMatches lists the service desk's customers whose email address equals the looked-up address.
# Atlassian can hold more than one account for one address, so the Flow decides which one it means.
@dataclass(frozen=True, slots=True)
class CustomerMatches:
    # Email is the address that was looked up.
    email: str = ''
    # Customers lists the matching customers in the provider's order; it is empty on notFound.
    customers: tuple[Customer, ...] = ()
    # HasMore reports that the provider's text match listed more than MaxCustomerMatches customers, so a
    # match beyond the first page was not compared.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'customers', _replace_nil_slices(self.customers, 1))


CustomerMatches.__annotations__['customers'] = tuple[Customer, ...] | None
CustomerMatches.__dataclass_fields__['customers'].type = tuple[Customer, ...] | None
setattr(CustomerMatches, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# SearchTicketsInput selects one page of one service desk's requests. ProjectKey is required; every
# other set filter narrows the search.
@dataclass(frozen=True, slots=True)
class SearchTicketsInput:
    # ProjectKey is the key of the service desk's Jira project, such as ITH, as the service desk picker
    # stores it.
    project_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectKey'})
    # StatusCategories matches requests in any of these status categories: new, indeterminate, or done.
    status_categories: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'statusCategories', 'dex_json_omit_default': True})
    # StatusNames matches requests in any of these workflow statuses, such as Waiting for support.
    status_names: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'statusNames', 'dex_json_omit_default': True})
    # Labels matches requests that carry at least one of these labels.
    labels: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ReporterAccountIDs matches requests raised for any of these customers, by Atlassian account ID.
    reporter_account_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'reporterAccountIds', 'dex_json_omit_default': True})
    # SummaryPhrase matches requests whose summary contains this phrase as Jira's text search tokenizes
    # it. Text search ignores case and punctuation, so compare the returned summaries for an exact match.
    summary_phrase: str = _dataclass_field(default='', metadata={'dex_json_name': 'summaryPhrase', 'dex_json_omit_default': True})
    # UpdatedSinceDate matches requests updated on or after this calendar date, written YYYY-MM-DD, in the
    # connected account's Jira time zone. Blank applies no time filter.
    updated_since_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'updatedSinceDate', 'dex_json_omit_default': True})
    # Order selects the result order; blank uses TicketSearchOrderCreatedDescending.
    order: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # PageSize is 1 to MaxSearchPageSize requests; zero uses DefaultSearchPageSize. Jira may return fewer.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # NextPageToken continues a previous search with the same filters; blank reads the first page.
    # Jira expires a token after seven days.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})


setattr(SearchTicketsInput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# TicketStatus is one Jira workflow status.
@dataclass(frozen=True, slots=True)
class TicketStatus:
    # ID is the status ID, stable across renames.
    id: str = ''
    # Name is the site's status name, such as Waiting for support.
    name: str = ''
    # CategoryKey is the status category: new, indeterminate, or done.
    category_key: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'categoryKey', 'dex_json_omit_default': True})


setattr(TicketStatus, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# PriorityReference identifies a priority.
@dataclass(frozen=True, slots=True)
class PriorityReference:
    # ID is the priority ID.
    id: str = ''
    # Name is the priority name, such as High.
    name: str = ''


setattr(PriorityReference, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# AccountReference identifies an Atlassian account without its email address.
@dataclass(frozen=True, slots=True)
class AccountReference:
    # AccountID is the Atlassian account ID, which createTicket accepts as RaiseOnBehalfOfAccountID.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    # DisplayName is the account's display name, which privacy settings may replace.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})


setattr(AccountReference, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# Ticket is the connector's view of one Jira Service Management customer request, read through the Jira
# issue that holds it. Status and priority names are the site's own values, never mapped to another
# vocabulary, and accounts carry IDs and display names but never email addresses.
@dataclass(frozen=True, slots=True)
class Ticket:
    # ID is the numeric issue ID, stable when the request moves between projects.
    id: str = ''
    # Key is the request key, such as ITH-12; it changes when the request moves.
    key: str = ''
    # ProjectKey is the key of the service desk's project, such as ITH.
    project_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'projectKey', 'dex_json_omit_default': True})
    # Summary is the one-line request title.
    summary: str = ''
    # Status is the request's current workflow status.
    status: TicketStatus = _dataclass_field(default_factory=TicketStatus)
    # Priority is the request priority, or nil when the project does not use priorities.
    priority: PriorityReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Assignee is the assigned agent, or nil when unassigned.
    assignee: AccountReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Reporter is the customer the request was raised for, or nil when Jira hides it.
    reporter: AccountReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Labels lists the request's labels, Jira's tags.
    labels: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Description is the plain text of the description, returned by getTicket only.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsDescriptionTruncated reports that Description stopped at 32767 characters.
    is_description_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDescriptionTruncated', 'dex_json_omit_default': True})
    # CreatedAt is when the request was raised.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the request last changed.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_format': 'rfc3339'})


setattr(Ticket, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# SearchTicketsOutput is one page of matching requests.
@dataclass(frozen=True, slots=True)
class SearchTicketsOutput:
    # JQL is the exact query sent to Jira.
    jql: str = ''
    # Tickets lists the page's requests without their Description.
    tickets: tuple[Ticket, ...] = ()
    # NextPageToken continues the search; it is empty on the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tickets', _replace_nil_slices(self.tickets, 1))


SearchTicketsOutput.__annotations__['tickets'] = tuple[Ticket, ...] | None
SearchTicketsOutput.__dataclass_fields__['tickets'].type = tuple[Ticket, ...] | None
setattr(SearchTicketsOutput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# GetTicketInput identifies one request and how many of its comments to read.
@dataclass(frozen=True, slots=True)
class GetTicketInput:
    # IssueIDOrKey is a request key such as ITH-12 or a numeric issue ID. Jira also finds a moved request
    # by its old key and returns its current key.
    issue_id_or_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueIdOrKey'})
    # CommentLimit is 1 to MaxCommentLimit comments to read from the start of the request's comments;
    # zero reads DefaultCommentLimit.
    comment_limit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'commentLimit', 'dex_json_omit_default': True})


setattr(GetTicketInput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# TicketComment is one public reply or internal note on a request.
@dataclass(frozen=True, slots=True)
class TicketComment:
    # ID is the comment ID.
    id: str = ''
    # IsPublic is true for a reply the customer can see and false for an internal note only agents see.
    is_public: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPublic'})
    # Body is the comment's stored text, in which Jira Service Management renders wiki markup.
    body: str = ''
    # IsBodyTruncated reports that Body stopped at 32767 characters.
    is_body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBodyTruncated', 'dex_json_omit_default': True})
    # Author is the agent or customer who wrote the comment, or nil when Jira hides it.
    author: AccountReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # CreatedAt is when the comment was added.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})


setattr(TicketComment, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# SLACycle is one SLA cycle. Durations are milliseconds; RemainingMillis is negative after a breach.
@dataclass(frozen=True, slots=True)
class SLACycle:
    # StartedAt is when the cycle started.
    started_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'startedAt', 'dex_json_format': 'rfc3339'})
    # BreachAt is when the cycle breaches or breached its goal, or nil when the provider gives none.
    breach_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'breachAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # StoppedAt is when a completed cycle stopped; it is nil for the ongoing cycle.
    stopped_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'stoppedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # IsBreached reports that the cycle exceeded its goal.
    is_breached: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBreached'})
    # IsPaused reports an ongoing cycle whose clock is paused, such as while waiting for the customer.
    is_paused: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPaused', 'dex_json_omit_default': True})
    # IsWithinCalendarHours reports an ongoing cycle whose clock runs only in the SLA's working hours.
    is_within_calendar_hours: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isWithinCalendarHours', 'dex_json_omit_default': True})
    # GoalDurationMillis is the goal in milliseconds.
    goal_duration_millis: int = _dataclass_field(default=0, metadata={'dex_json_name': 'goalDurationMillis'})
    # ElapsedMillis is the clock time used in milliseconds.
    elapsed_millis: int = _dataclass_field(default=0, metadata={'dex_json_name': 'elapsedMillis'})
    # RemainingMillis is the clock time left in milliseconds.
    remaining_millis: int = _dataclass_field(default=0, metadata={'dex_json_name': 'remainingMillis'})


setattr(SLACycle, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# TicketSLA is one service level agreement on a request, with Jira Service Management's own SLA name.
@dataclass(frozen=True, slots=True)
class TicketSLA:
    # ID is the SLA ID.
    id: str = ''
    # Name is the SLA name, such as Time to resolution.
    name: str = ''
    # OngoingCycle is the running cycle, or nil when the SLA is not running.
    ongoing_cycle: SLACycle | None = _dataclass_field(default=None, metadata={'dex_json_name': 'ongoingCycle', 'dex_json_omit_default': True})
    # CompletedCycles lists up to MaxSLACompletedCycles finished cycles.
    completed_cycles: tuple[SLACycle, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'completedCycles', 'dex_json_omit_default': True})


setattr(TicketSLA, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# TicketDetails is one request with its first comments and its SLAs.
@dataclass(frozen=True, slots=True)
class TicketDetails:
    # Ticket is the request with its plain-text description.
    ticket: Ticket = _dataclass_field(default_factory=Ticket)
    # Comments lists public replies and internal notes in the order Jira Service Management returns them.
    comments: tuple[TicketComment, ...] = ()
    # HasMoreComments reports that the request has comments beyond Comments.
    has_more_comments: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMoreComments', 'dex_json_omit_default': True})
    # SLAs lists the request's service level agreements, such as Time to first response.
    slas: tuple[TicketSLA, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'comments', _replace_nil_slices(self.comments, 1))
        object.__setattr__(self, 'slas', _replace_nil_slices(self.slas, 1))


TicketDetails.__annotations__['comments'] = tuple[TicketComment, ...] | None
TicketDetails.__dataclass_fields__['comments'].type = tuple[TicketComment, ...] | None
TicketDetails.__annotations__['slas'] = tuple[TicketSLA, ...] | None
TicketDetails.__dataclass_fields__['slas'].type = tuple[TicketSLA, ...] | None
setattr(TicketDetails, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# CreateTicketInput is one new customer request. The request type decides which fields Jira Service
# Management requires; GET /rest/servicedeskapi/servicedesk/{serviceDeskId}/requesttype/{requestTypeId}/field
# lists them.
@dataclass(frozen=True, slots=True)
class CreateTicketInput:
    # ServiceDeskID is the numeric service desk ID, as the request type picker stores it.
    service_desk_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'serviceDeskId'})
    # RequestTypeID is the numeric request type ID, as the request type picker stores it.
    request_type_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'requestTypeId'})
    # Summary is the one-line request title, at most 255 characters.
    summary: str = ''
    # Description is the request's text, at most 32767 characters; blank sends none. Jira Service
    # Management stores it as text in which it renders wiki markup, so characters such as * and _ can
    # format the text; the connector sends it unchanged.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # RaiseOnBehalfOfAccountID raises the request for this customer, by Atlassian account ID such as one
    # findCustomerByEmail returned; blank raises it as the connected agent.
    raise_on_behalf_of_account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'raiseOnBehalfOfAccountId', 'dex_json_omit_default': True})
    # AdditionalFieldValues sets up to MaxAdditionalFieldValues more request-type fields by Jira field ID,
    # such as customfield_10050, each as the JSON value Jira's field input format expects. It cannot set
    # summary or description.
    additional_field_values: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'additionalFieldValues', 'dex_json_omit_default': True})


setattr(CreateTicketInput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# RequestStatus is a customer request's status as the request API reports it.
@dataclass(frozen=True, slots=True)
class RequestStatus:
    # Name is the status name, such as Waiting for support.
    name: str = ''
    # Category is the request API's status category.
    category: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(RequestStatus, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# CreateTicketOutput identifies the created request. On providerRejected and uncertain, IssueID and
# IssueKey are empty and the requested desk, request type, summary, and customer are echoed so the
# application can reconcile.
@dataclass(frozen=True, slots=True)
class CreateTicketOutput:
    # IssueID is the new request's numeric issue ID.
    issue_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueId', 'dex_json_omit_default': True})
    # IssueKey is the new request's key, such as ITH-12.
    issue_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueKey', 'dex_json_omit_default': True})
    # ServiceDeskID echoes the requested service desk.
    service_desk_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'serviceDeskId'})
    # RequestTypeID echoes the requested request type.
    request_type_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'requestTypeId'})
    # Summary echoes the requested summary.
    summary: str = ''
    # RaiseOnBehalfOfAccountID echoes the customer the request was raised for.
    raise_on_behalf_of_account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'raiseOnBehalfOfAccountId', 'dex_json_omit_default': True})
    # RequestStatus is the new request's Jira Service Management status, or nil when the provider gave none.
    request_status: RequestStatus | None = _dataclass_field(default=None, metadata={'dex_json_name': 'requestStatus', 'dex_json_omit_default': True})
    # CreatedAt is when the request was raised, or nil when the provider gave no date.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # RejectedFieldIDs lists the field IDs a rejection named.
    rejected_field_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'rejectedFieldIds', 'dex_json_omit_default': True})
    # ProviderErrorKey is the machine-readable i18n key of a rejection, when the provider sent a safe one.
    provider_error_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'providerErrorKey', 'dex_json_omit_default': True})


setattr(CreateTicketOutput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# UpdateTicketInput adds or removes labels on one request and sets its priority. Set at least one change.
@dataclass(frozen=True, slots=True)
class UpdateTicketInput:
    # IssueIDOrKey is a request key such as ITH-12 or a numeric issue ID.
    issue_id_or_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueIdOrKey'})
    # AddLabels adds these labels, without whitespace, and keeps the request's other labels.
    add_labels: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'addLabels', 'dex_json_omit_default': True})
    # RemoveLabels removes these labels and keeps the request's other labels. Labels compare exactly.
    remove_labels: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removeLabels', 'dex_json_omit_default': True})
    # PriorityName sets the priority by the site's name, such as High, compared without case; blank keeps it.
    priority_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'priorityName', 'dex_json_omit_default': True})


setattr(UpdateTicketInput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# UpdateTicketOutput is the request's labels and priority after the change.
@dataclass(frozen=True, slots=True)
class UpdateTicketOutput:
    # IssueID is the request's numeric issue ID.
    issue_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueId', 'dex_json_omit_default': True})
    # IssueKey is the request's current key.
    issue_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueKey', 'dex_json_omit_default': True})
    # Labels lists the request's labels after the change.
    labels: tuple[str, ...] = ()
    # Priority is the request's priority after the change, or nil when it has none. After a write that set
    # the priority, only its Name is known.
    priority: PriorityReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # WasAlreadyApplied reports that the request already held every requested value, written by an earlier
    # attempt or by someone else, so this attempt wrote nothing.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})
    # RejectedFieldIDs lists the field IDs Jira named in a rejection, such as priority.
    rejected_field_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'rejectedFieldIds', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'labels', _replace_nil_slices(self.labels, 1))


UpdateTicketOutput.__annotations__['labels'] = tuple[str, ...] | None
UpdateTicketOutput.__dataclass_fields__['labels'].type = tuple[str, ...] | None
setattr(UpdateTicketOutput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# TransitionTicketInput selects one Jira workflow transition of a request. Set at least one of TransitionID,
# TransitionName, and DestinationStatusName; every one that is set must match the same available transition.
@dataclass(frozen=True, slots=True)
class TransitionTicketInput:
    # IssueIDOrKey is a request key such as ITH-12 or a numeric issue ID.
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


setattr(TransitionTicketInput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# TransitionReference is one workflow transition available on an issue.
@dataclass(frozen=True, slots=True)
class TransitionReference:
    # ID is the transition ID.
    id: str = ''
    # Name is the transition name.
    name: str = ''
    # DestinationStatus is the status the transition leads to.
    destination_status: TicketStatus = _dataclass_field(default_factory=TicketStatus, metadata={'dex_json_name': 'destinationStatus'})


setattr(TransitionReference, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# TransitionTicketOutput describes the issue's workflow position after the operation.
@dataclass(frozen=True, slots=True)
class TransitionTicketOutput:
    # IssueID is the issue's numeric ID.
    issue_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueId'})
    # IssueKey is the issue's current key.
    issue_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueKey'})
    # PreviousStatus is the status the operation read before it acted.
    previous_status: TicketStatus = _dataclass_field(default_factory=TicketStatus, metadata={'dex_json_name': 'previousStatus'})
    # Status is the issue's status after the operation.
    status: TicketStatus = _dataclass_field(default_factory=TicketStatus)
    # Transition is the matched transition, or nil when none matched.
    transition: TransitionReference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # IsAlreadyInDestinationStatus reports that the issue was already in the destination, so nothing was sent.
    is_already_in_destination_status: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAlreadyInDestinationStatus', 'dex_json_omit_default': True})
    # AvailableTransitions lists up to 50 transitions available from Status, set on transitionUnavailable.
    available_transitions: tuple[TransitionReference, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'availableTransitions', 'dex_json_omit_default': True})
    # RejectedFieldIDs lists the field IDs Jira named in a rejection, such as resolution.
    rejected_field_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'rejectedFieldIds', 'dex_json_omit_default': True})


setattr(TransitionTicketOutput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# AddCommentInput is one public reply or internal note on a request.
@dataclass(frozen=True, slots=True)
class AddCommentInput:
    # IssueIDOrKey is a request key such as ITH-12 or a numeric issue ID.
    issue_id_or_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueIdOrKey'})
    # Body is the comment's text, at most 32767 characters. Jira Service Management renders wiki markup
    # in it, so characters such as * and _ can format the text; the connector sends it unchanged.
    body: str = ''
    # IsPublic adds a public reply that Jira Service Management shares with the customer and notifies
    # them of. False adds an internal note that only agents see. Visibility is fixed when it is added.
    is_public: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPublic', 'dex_json_omit_default': True})


setattr(AddCommentInput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')


# AddCommentOutput identifies the added comment. On notFound, providerRejected, and uncertain, CommentID
# is empty and the request and visibility are echoed.
@dataclass(frozen=True, slots=True)
class AddCommentOutput:
    # IssueIDOrKey echoes the request.
    issue_id_or_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'issueIdOrKey'})
    # CommentID is the new comment's ID.
    comment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'commentId', 'dex_json_omit_default': True})
    # IsPublic is the comment's visibility as requested, or as the provider confirmed it on added.
    is_public: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPublic'})
    # CreatedAt is when the comment was added, or nil when the provider gave no date.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(AddCommentOutput, '__module__', 'dex_connectors.atlassian_jira_service_management.models')
