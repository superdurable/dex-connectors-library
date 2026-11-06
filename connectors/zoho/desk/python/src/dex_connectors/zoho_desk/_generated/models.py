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


# SearchTicketsInput selects tickets with typed filters that the connector turns into Zoho Desk
# search parameters. Different filters narrow each other; several statuses, status types, or
# priorities match any of them. At least one filter is required.
@dataclass(frozen=True, slots=True)
class SearchTicketsInput:
    # Statuses matches tickets in any of these Zoho Desk status names, including custom statuses,
    # such as Open or Waiting for Customer. Set Statuses or StatusTypes, not both.
    statuses: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # StatusTypes matches tickets whose status falls under any of these types, so custom statuses
    # match without listing them. Set Statuses or StatusTypes, not both.
    status_types: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'statusTypes', 'dex_json_omit_default': True})
    # Priorities matches tickets with any of these Zoho Desk priority names, such as High.
    priorities: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ContactEmail keeps only tickets whose email or contact email is this address, compared without
    # regard to letter case, such as jane@example.com. Zoho Desk's email filter also matches
    # look-alike addresses, so the connector checks each ticket itself; a page can then hold fewer
    # tickets than Limit, even none, while NextFrom is set. Blank keeps every contact.
    contact_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactEmail', 'dex_json_omit_default': True})
    # DepartmentID matches tickets in this department. Blank searches every department the agent can see.
    department_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'departmentId', 'dex_json_omit_default': True})
    # ModifiedSince matches tickets modified at or after this RFC 3339 instant with an explicit
    # offset, such as 2026-01-21T00:00:00Z, until the time of the request. Blank applies no time filter.
    modified_since: str = _dataclass_field(default='', metadata={'dex_json_name': 'modifiedSince', 'dex_json_omit_default': True})
    # From is the zero-based index of the first result, 0 to 4999.
    from_: int = _dataclass_field(default=0, metadata={'dex_json_name': 'from', 'dex_json_omit_default': True})
    # Limit is 1 to MaxSearchLimit; zero uses DefaultSearchLimit. A page that would pass result
    # MaxSearchResults is shortened to end there.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(SearchTicketsInput, '__module__', 'dex_connectors.zoho_desk.models')


# Ticket is the connector's view of one Zoho Desk ticket. Custom fields, phone numbers, shared
# departments, and blueprint details are omitted. IDs are Zoho Desk's numeric IDs as decimal
# strings, and an empty ID means Zoho Desk reported none, such as an unassigned ticket.
@dataclass(frozen=True, slots=True)
class Ticket:
    # ID is the Zoho Desk ticket ID.
    id: str = ''
    # TicketNumber is the ticket's serial number that agents see, such as 101.
    ticket_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'ticketNumber', 'dex_json_omit_default': True})
    # Subject is the ticket subject.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DescriptionHTML is the ticket description as the HTML Zoho Desk stores. searchTickets leaves
    # it empty to keep pages small.
    description_html: str = _dataclass_field(default='', metadata={'dex_json_name': 'descriptionHtml', 'dex_json_omit_default': True})
    # IsDescriptionTruncated reports that DescriptionHTML was cut at MaxTextBytes.
    is_description_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDescriptionTruncated', 'dex_json_omit_default': True})
    # Status is Zoho Desk's status name, including custom statuses.
    status: str = _dataclass_field(kw_only=True)
    # StatusType is the status's category. The connector reports Zoho Desk's spellings OPEN,
    # ONHOLD, and CLOSED as the TicketStatusType constants and passes any other value through.
    status_type: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'statusType', 'dex_json_omit_default': True})
    # Priority is Zoho Desk's priority name, or empty when the ticket has none.
    priority: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Channel is the channel that created the ticket, such as Email, Web, or Phone.
    channel: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Classification is the ticket's classification, such as Problem or Question, or empty.
    classification: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DepartmentID is the ticket's department.
    department_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'departmentId', 'dex_json_omit_default': True})
    # ContactID is the contact who raised the ticket.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId', 'dex_json_omit_default': True})
    # AssigneeID is the assigned agent, or empty when unassigned.
    assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # TeamID is the assigned team, or empty.
    team_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamId', 'dex_json_omit_default': True})
    # Email is the ticket's email address, which Zoho Desk's email search filter matches.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsEscalated reports that Zoho Desk escalated the ticket.
    is_escalated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isEscalated', 'dex_json_omit_default': True})
    # IsOverdue reports that the ticket is past its due date.
    is_overdue: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOverdue', 'dex_json_omit_default': True})
    # IsSpam reports that the ticket is marked as spam.
    is_spam: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSpam', 'dex_json_omit_default': True})
    # ThreadCount is Zoho Desk's count of the ticket's threads.
    thread_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'threadCount', 'dex_json_omit_default': True})
    # CommentCount is Zoho Desk's count of the ticket's comments.
    comment_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'commentCount', 'dex_json_omit_default': True})
    # CreatedAt is when the ticket was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # ModifiedAt is when the ticket last changed.
    modified_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'modifiedAt', 'dex_json_format': 'rfc3339'})
    # DueAt is when the ticket is due, or nil.
    due_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'dueAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # ClosedAt is when the ticket was closed, or nil.
    closed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'closedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # WebURL is the agent interface link to the ticket.
    web_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'webUrl', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')
        if isinstance(self.modified_at, str):
            object.__setattr__(self, 'modified_at', parse_go_datetime(self.modified_at, 'modified_at'))
        elif self.modified_at is not None:
            validate_go_datetime(self.modified_at, 'modified_at')
        if isinstance(self.due_at, str):
            object.__setattr__(self, 'due_at', parse_go_datetime(self.due_at, 'due_at'))
        elif self.due_at is not None:
            validate_go_datetime(self.due_at, 'due_at')
        if isinstance(self.closed_at, str):
            object.__setattr__(self, 'closed_at', parse_go_datetime(self.closed_at, 'closed_at'))
        elif self.closed_at is not None:
            validate_go_datetime(self.closed_at, 'closed_at')


setattr(Ticket, '__module__', 'dex_connectors.zoho_desk.models')


# SearchTicketsOutput is one page of matching tickets, most recently modified first. Zoho Desk
# indexes new and changed tickets with a delay, so a page can omit a change made moments earlier.
@dataclass(frozen=True, slots=True)
class SearchTicketsOutput:
    # Tickets lists the matching tickets without their DescriptionHTML.
    tickets: tuple[Ticket, ...] = ()
    # TotalMatched is Zoho Desk's reported count of matching tickets, before the ContactEmail check.
    total_matched: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalMatched'})
    # NextFrom is the From of the next page, or zero when this page is the last that Zoho Desk serves.
    next_from: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextFrom', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tickets', _replace_nil_slices(self.tickets, 1))


SearchTicketsOutput.__annotations__['tickets'] = tuple[Ticket, ...] | None
SearchTicketsOutput.__dataclass_fields__['tickets'].type = tuple[Ticket, ...] | None
setattr(SearchTicketsOutput, '__module__', 'dex_connectors.zoho_desk.models')


# GetTicketInput names one ticket and how many of its newest threads and comments to return.
@dataclass(frozen=True, slots=True)
class GetTicketInput:
    # TicketID is the Zoho Desk ticket ID, a decimal string such as 1892000000042034.
    ticket_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'ticketId'})
    # ThreadLimit is 1 to MaxThreadLimit; zero uses DefaultThreadLimit.
    thread_limit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'threadLimit', 'dex_json_omit_default': True})
    # CommentLimit is 1 to MaxCommentLimit; zero uses DefaultCommentLimit.
    comment_limit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'commentLimit', 'dex_json_omit_default': True})


setattr(GetTicketInput, '__module__', 'dex_connectors.zoho_desk.models')


# TicketContact is the contact who raised a ticket.
@dataclass(frozen=True, slots=True)
class TicketContact:
    # ID is the Zoho Desk contact ID.
    id: str = ''
    # FirstName is the contact's first name, or empty.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName', 'dex_json_omit_default': True})
    # LastName is the contact's last name, or empty.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName', 'dex_json_omit_default': True})
    # Email is the contact's email address, or empty when Zoho Desk has none.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(TicketContact, '__module__', 'dex_connectors.zoho_desk.models')


# TicketThread is one message in a ticket's conversation, such as a customer email or an agent
# reply. Zoho Desk's thread list carries a summary, not the full content.
@dataclass(frozen=True, slots=True)
class TicketThread:
    # ID is the Zoho Desk thread ID.
    id: str = ''
    # Channel is the thread's channel, such as EMAIL, FORUMS, or FEEDBACK.
    channel: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Direction is in for a message from the customer and out for one to the customer.
    direction: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Visibility is public or private.
    visibility: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Status is Zoho Desk's delivery status, such as SUCCESS, FAILED, or DRAFT.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Summary is Zoho Desk's plain-text summary of the thread.
    summary: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsSummaryTruncated reports that Summary was cut at MaxTextBytes.
    is_summary_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSummaryTruncated', 'dex_json_omit_default': True})
    # IsDescriptionThread reports the thread that holds the ticket's original description.
    is_description_thread: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDescriptionThread', 'dex_json_omit_default': True})
    # AuthorName is the author's display name.
    author_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorName', 'dex_json_omit_default': True})
    # AuthorType is AGENT or END_USER.
    author_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorType', 'dex_json_omit_default': True})
    # CreatedAt is when the thread was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')


setattr(TicketThread, '__module__', 'dex_connectors.zoho_desk.models')


# TicketComment is one public or private comment on a ticket.
@dataclass(frozen=True, slots=True)
class TicketComment:
    # ID is the Zoho Desk comment ID.
    id: str = ''
    # IsPublic is true for a comment the customer can see in the help center and false for a
    # private comment only agents see.
    is_public: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPublic'})
    # Content is the comment as Zoho Desk returned it, in ContentType.
    content: str = ''
    # ContentType is plainText or html.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType', 'dex_json_omit_default': True})
    # IsContentTruncated reports that Content was cut at MaxTextBytes.
    is_content_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isContentTruncated', 'dex_json_omit_default': True})
    # CommenterID is the agent or user who commented.
    commenter_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'commenterId', 'dex_json_omit_default': True})
    # CommenterName is the commenter's display name.
    commenter_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'commenterName', 'dex_json_omit_default': True})
    # CommenterType is AGENT or END_USER.
    commenter_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'commenterType', 'dex_json_omit_default': True})
    # CommentedAt is when the comment was added.
    commented_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'commentedAt', 'dex_json_format': 'rfc3339'})
    # ModifiedAt is when the comment was last edited, or nil.
    modified_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'modifiedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.commented_at, str):
            object.__setattr__(self, 'commented_at', parse_go_datetime(self.commented_at, 'commented_at'))
        elif self.commented_at is not None:
            validate_go_datetime(self.commented_at, 'commented_at')
        if isinstance(self.modified_at, str):
            object.__setattr__(self, 'modified_at', parse_go_datetime(self.modified_at, 'modified_at'))
        elif self.modified_at is not None:
            validate_go_datetime(self.modified_at, 'modified_at')


setattr(TicketComment, '__module__', 'dex_connectors.zoho_desk.models')


# TicketDetails is one ticket with its contact, newest threads, and newest comments.
@dataclass(frozen=True, slots=True)
class TicketDetails:
    # Ticket is the ticket, including its HTML description.
    ticket: Ticket
    # Contact is the contact who raised the ticket, or nil when Zoho Desk embedded none.
    contact: TicketContact | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Threads lists the ticket's newest threads, newest first: emails, replies, and other
    # conversation messages, each with Zoho Desk's plain-text summary.
    threads: tuple[TicketThread, ...] = ()
    # HasMoreThreads reports that the ticket has older threads than the last one returned.
    has_more_threads: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMoreThreads', 'dex_json_omit_default': True})
    # Comments lists the ticket's newest comments, newest first, public and private alike.
    comments: tuple[TicketComment, ...] = ()
    # HasMoreComments reports that the ticket has older comments than the last one returned.
    has_more_comments: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMoreComments', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'threads', _replace_nil_slices(self.threads, 1))
        object.__setattr__(self, 'comments', _replace_nil_slices(self.comments, 1))


TicketDetails.__annotations__['threads'] = tuple[TicketThread, ...] | None
TicketDetails.__dataclass_fields__['threads'].type = tuple[TicketThread, ...] | None
TicketDetails.__annotations__['comments'] = tuple[TicketComment, ...] | None
TicketDetails.__dataclass_fields__['comments'].type = tuple[TicketComment, ...] | None
setattr(TicketDetails, '__module__', 'dex_connectors.zoho_desk.models')


# TicketContactInput identifies a ticket's contact by email for createTicket.
@dataclass(frozen=True, slots=True)
class TicketContactInput:
    # Email is one bare address such as jane@example.com.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # FirstName names a contact Zoho Desk adds; blank sends none.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName', 'dex_json_omit_default': True})
    # LastName names a contact Zoho Desk adds; blank sends none.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName', 'dex_json_omit_default': True})


setattr(TicketContactInput, '__module__', 'dex_connectors.zoho_desk.models')


# CreateTicketInput is one new ticket in a department. Zero values leave a field to Zoho Desk's
# defaults, the department's layout, and its automations.
@dataclass(frozen=True, slots=True)
class CreateTicketInput:
    # Subject is the ticket subject, at most 255 characters. It is required.
    subject: str = ''
    # Description is the ticket's plain-text content, at most 65,535 characters. It is required.
    # The connector escapes it into Zoho Desk's HTML description, so markup is shown literally and
    # line breaks are kept.
    description: str = ''
    # DepartmentID is the department that owns the ticket. Zoho Desk requires it; GET
    # /api/v1/departments lists the organization's departments.
    department_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'departmentId'})
    # ContactID names an existing contact. Set ContactID or Contact.Email, not both.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId', 'dex_json_omit_default': True})
    # Contact identifies the customer by email. Zoho Desk uses the contact with that address or,
    # when none exists, adds one with the given names.
    contact: TicketContactInput | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Status is a Zoho Desk status name, including custom statuses, or empty for the department's default.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Priority is a Zoho Desk priority name, or empty for the department's default.
    priority: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # AssigneeID assigns the ticket to an agent, or empty for Zoho Desk's routing.
    assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})


setattr(CreateTicketInput, '__module__', 'dex_connectors.zoho_desk.models')


# CreateTicketOutput is the created ticket.
@dataclass(frozen=True, slots=True)
class CreateTicketOutput:
    # Ticket is the ticket Zoho Desk created.
    ticket: Ticket


setattr(CreateTicketOutput, '__module__', 'dex_connectors.zoho_desk.models')


# UpdateTicketInput is one change to an existing ticket. Empty fields are left unchanged; this
# release cannot clear an assignee.
@dataclass(frozen=True, slots=True)
class UpdateTicketInput:
    # TicketID is the Zoho Desk ticket ID.
    ticket_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'ticketId'})
    # Status is the new Zoho Desk status name, including a custom one, or empty to keep it.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Priority is the new Zoho Desk priority name, or empty to keep it.
    priority: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # AssigneeID assigns an agent of the ticket's department, or empty to keep the assignee.
    assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # DepartmentID moves the ticket to another department, or empty to keep it. Zoho Desk applies
    # the new department's rules, so set AssigneeID too when the agent must stay assigned.
    department_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'departmentId', 'dex_json_omit_default': True})


setattr(UpdateTicketInput, '__module__', 'dex_connectors.zoho_desk.models')


# UpdateTicketOutput is the ticket after the change.
@dataclass(frozen=True, slots=True)
class UpdateTicketOutput:
    # Ticket is the ticket Zoho Desk returned after the write, or as read when nothing needed writing.
    ticket: Ticket
    # WasAlreadyApplied reports that the ticket already held every requested value, for example
    # because an earlier attempt of this Step wrote them, so this attempt wrote nothing.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(UpdateTicketOutput, '__module__', 'dex_connectors.zoho_desk.models')


# AddCommentInput is one plain-text comment to add to a ticket.
@dataclass(frozen=True, slots=True)
class AddCommentInput:
    # TicketID is the Zoho Desk ticket ID.
    ticket_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'ticketId'})
    # Content is the plain-text comment, at most MaxCommentRunes characters. It is required. The
    # connector sends it as plainText, so markup is shown literally.
    content: str = ''
    # IsPublic adds a public comment, which the customer can see in the help center. False adds a
    # private comment that only agents see. Zoho Desk fixes visibility when the comment is added.
    is_public: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPublic', 'dex_json_omit_default': True})


setattr(AddCommentInput, '__module__', 'dex_connectors.zoho_desk.models')


# AddCommentOutput is the added comment.
@dataclass(frozen=True, slots=True)
class AddCommentOutput:
    # Comment is the comment Zoho Desk added.
    comment: TicketComment = _dataclass_field(default_factory=TicketComment)


setattr(AddCommentOutput, '__module__', 'dex_connectors.zoho_desk.models')
