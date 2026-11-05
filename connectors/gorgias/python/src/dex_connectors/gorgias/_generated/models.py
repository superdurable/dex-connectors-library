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


# SearchTicketsInput lists tickets most recently updated first. RequesterEmail, RequesterID, and
# ViewID narrow the list on Gorgias's side; Statuses, Priorities, Tags, and UpdatedSince filter
# each page after Gorgias returns it, so a page can hold fewer than PageSize tickets, even none,
# while NextCursor is set. Several statuses, priorities, or tags match any of them.
@dataclass(frozen=True, slots=True)
class SearchTicketsInput:
    # RequesterEmail lists only the tickets of the customer whose primary email address this is,
    # such as jane@example.com. A customer that does not exist yields an empty page.
    requester_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'requesterEmail', 'dex_json_omit_default': True})
    # RequesterID lists only the tickets of this Gorgias customer. Use it or RequesterEmail, not both.
    requester_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'requesterId', 'dex_json_omit_default': True})
    # ViewID lists only the tickets matching the filters of this Gorgias view, such as one an
    # administrator built for a status and tag; Gorgias then orders the page by the view's sort.
    view_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'viewId', 'dex_json_omit_default': True})
    # Statuses keeps tickets in any of these Gorgias statuses. Empty keeps every status.
    statuses: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Priorities keeps tickets with any of these Gorgias priorities. Empty keeps every priority.
    priorities: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Tags keeps tickets carrying any of these tag names, compared case sensitively as Gorgias does.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # UpdatedSince keeps tickets updated at or after this RFC 3339 instant with an explicit offset,
    # such as 2026-01-21T00:00:00Z. Without ViewID it also ends paging at the first older ticket.
    updated_since: str = _dataclass_field(default='', metadata={'dex_json_name': 'updatedSince', 'dex_json_omit_default': True})
    # PageSize is 1 to MaxSearchPageSize tickets listed from Gorgias; zero uses DefaultSearchPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Cursor is a previous page's NextCursor, read with the same filters.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(SearchTicketsInput, '__module__', 'dex_connectors.gorgias.models')


# Ticket is the connector's view of one Gorgias ticket. Custom fields, integrations data,
# events, and satisfaction surveys are omitted. Zero IDs mean Gorgias reported none, such as
# an unassigned ticket.
@dataclass(frozen=True, slots=True)
class Ticket:
    # ID is the Gorgias ticket ID.
    id: int = 0
    # Subject is the ticket subject.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Status is Gorgias's status, open or closed, passed through unchanged.
    status: str = _dataclass_field(kw_only=True)
    # Priority is Gorgias's priority, or empty when the response carried none.
    priority: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Channel is how the conversation started, such as email, chat, or api.
    channel: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Via is how the first message was received or sent, such as email or api.
    via: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # RequesterID is the Gorgias customer the ticket belongs to.
    requester_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'requesterId', 'dex_json_omit_default': True})
    # RequesterEmail is that customer's primary email address, or empty when Gorgias has none.
    requester_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'requesterEmail', 'dex_json_omit_default': True})
    # AssigneeUserID is the assigned agent, or zero when unassigned.
    assignee_user_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'assigneeUserId', 'dex_json_omit_default': True})
    # AssigneeTeamID is the assigned team, or zero when unassigned.
    assignee_team_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'assigneeTeamId', 'dex_json_omit_default': True})
    # Tags lists the ticket's tag names; Gorgias tag names are case sensitive.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ExternalID is the ticket's external ID. Tickets createTicket creates carry its idempotency key.
    external_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'externalId', 'dex_json_omit_default': True})
    # Language is the ISO 639-1 language Gorgias detected or was given, or empty.
    language: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsSpam reports that the ticket is marked as spam.
    is_spam: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSpam', 'dex_json_omit_default': True})
    # MessageCount is Gorgias's count of the ticket's messages, reported only by searchTickets.
    message_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'messageCount', 'dex_json_omit_default': True})
    # CreatedAt is when the ticket was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the ticket last changed; it equals CreatedAt when Gorgias reports none.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_format': 'rfc3339'})
    # LastReceivedMessageAt is when the customer last wrote, or nil.
    last_received_message_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'lastReceivedMessageAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # LastMessageAt is when the last message was sent, or nil.
    last_message_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'lastMessageAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # ClosedAt is when the ticket was closed, or nil.
    closed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'closedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # SnoozedUntil is when Gorgias reopens a snoozed ticket, or nil.
    snoozed_until: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'snoozedUntil', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # TrashedAt is when the ticket was moved to the trash, or nil.
    trashed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'trashedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime(self.updated_at, 'updated_at'))
        elif self.updated_at is not None:
            validate_go_datetime(self.updated_at, 'updated_at')
        if isinstance(self.last_received_message_at, str):
            object.__setattr__(self, 'last_received_message_at', parse_go_datetime(self.last_received_message_at, 'last_received_message_at'))
        elif self.last_received_message_at is not None:
            validate_go_datetime(self.last_received_message_at, 'last_received_message_at')
        if isinstance(self.last_message_at, str):
            object.__setattr__(self, 'last_message_at', parse_go_datetime(self.last_message_at, 'last_message_at'))
        elif self.last_message_at is not None:
            validate_go_datetime(self.last_message_at, 'last_message_at')
        if isinstance(self.closed_at, str):
            object.__setattr__(self, 'closed_at', parse_go_datetime(self.closed_at, 'closed_at'))
        elif self.closed_at is not None:
            validate_go_datetime(self.closed_at, 'closed_at')
        if isinstance(self.snoozed_until, str):
            object.__setattr__(self, 'snoozed_until', parse_go_datetime(self.snoozed_until, 'snoozed_until'))
        elif self.snoozed_until is not None:
            validate_go_datetime(self.snoozed_until, 'snoozed_until')
        if isinstance(self.trashed_at, str):
            object.__setattr__(self, 'trashed_at', parse_go_datetime(self.trashed_at, 'trashed_at'))
        elif self.trashed_at is not None:
            validate_go_datetime(self.trashed_at, 'trashed_at')


setattr(Ticket, '__module__', 'dex_connectors.gorgias.models')


# SearchTicketsOutput is one page of matching tickets.
@dataclass(frozen=True, slots=True)
class SearchTicketsOutput:
    # Tickets lists the matching tickets in Gorgias's order, without their messages.
    tickets: tuple[Ticket, ...] = ()
    # NextCursor reads the next page; it is empty on the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})
    # ListedCount is the number of tickets Gorgias listed on this page before the connector's filters.
    listed_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'listedCount'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tickets', _replace_nil_slices(self.tickets, 1))


SearchTicketsOutput.__annotations__['tickets'] = tuple[Ticket, ...] | None
SearchTicketsOutput.__dataclass_fields__['tickets'].type = tuple[Ticket, ...] | None
setattr(SearchTicketsOutput, '__module__', 'dex_connectors.gorgias.models')


# GetTicketInput names one ticket and how many of its latest messages to return.
@dataclass(frozen=True, slots=True)
class GetTicketInput:
    # TicketID is the Gorgias ticket ID.
    ticket_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'ticketId'})
    # LatestMessageLimit is 1 to MaxLatestMessageLimit; zero uses DefaultLatestMessageLimit.
    latest_message_limit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'latestMessageLimit', 'dex_json_omit_default': True})


setattr(GetTicketInput, '__module__', 'dex_connectors.gorgias.models')


# Customer is the connector's view of one Gorgias customer, the requester of a ticket.
@dataclass(frozen=True, slots=True)
class Customer:
    # ID is the Gorgias customer ID, which searchTickets accepts as RequesterID.
    id: int = 0
    # Email is the customer's primary email address, or empty.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Name is the customer's full name, or empty.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ExternalID is the customer's ID in another system, such as a CRM, or empty.
    external_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'externalId', 'dex_json_omit_default': True})
    # Language is the customer's preferred ISO 639-1 language, or empty.
    language: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(Customer, '__module__', 'dex_connectors.gorgias.models')


# TicketMessage is one message on a ticket: a customer message, an agent reply, or an internal note.
@dataclass(frozen=True, slots=True)
class TicketMessage:
    # ID is the Gorgias message ID.
    id: int = 0
    # TicketID is the ticket the message belongs to.
    ticket_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'ticketId'})
    # Channel is the message channel, such as MessageChannelEmail or MessageChannelInternalNote.
    channel: str = ''
    # Via is how the message was received or sent, such as email or api.
    via: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsPublic is false for an internal note, which only agents see.
    is_public: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPublic'})
    # IsFromAgent is true for a message your company sent, false for a customer's message.
    is_from_agent: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFromAgent'})
    # SenderID is the user or customer who sent the message, or zero.
    sender_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'senderId', 'dex_json_omit_default': True})
    # Subject is the message subject, or empty.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Body is the message as plain text: Gorgias's body_text, or its stripped_text when that is empty.
    body: str = ''
    # IsBodyTruncated reports that Body was cut at MaxTextBytes.
    is_body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBodyTruncated', 'dex_json_omit_default': True})
    # ExternalID is the message's external ID. Messages addNote adds carry its idempotency key.
    external_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'externalId', 'dex_json_omit_default': True})
    # CreatedAt is when the message was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # SentAt is when Gorgias sent the message, or nil. Gorgias sends replies asynchronously, so a
    # reply addNote just added has none yet.
    sent_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'sentAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # FailedAt is when Gorgias failed to send the message, or nil.
    failed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'failedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')
        if isinstance(self.sent_at, str):
            object.__setattr__(self, 'sent_at', parse_go_datetime(self.sent_at, 'sent_at'))
        elif self.sent_at is not None:
            validate_go_datetime(self.sent_at, 'sent_at')
        if isinstance(self.failed_at, str):
            object.__setattr__(self, 'failed_at', parse_go_datetime(self.failed_at, 'failed_at'))
        elif self.failed_at is not None:
            validate_go_datetime(self.failed_at, 'failed_at')


setattr(TicketMessage, '__module__', 'dex_connectors.gorgias.models')


# TicketDetails is one ticket with its customer and latest messages.
@dataclass(frozen=True, slots=True)
class TicketDetails:
    # Ticket is the ticket, including its tags.
    ticket: Ticket
    # Requester is the ticket's customer, or nil when Gorgias did not embed one.
    requester: Customer | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # LatestMessages lists the newest messages first: customer messages, replies, and internal notes alike.
    latest_messages: tuple[TicketMessage, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'latestMessages'})
    # HasOlderMessages reports that the ticket has messages older than LatestMessages.
    has_older_messages: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasOlderMessages', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'latest_messages', _replace_nil_slices(self.latest_messages, 1))


TicketDetails.__annotations__['latest_messages'] = tuple[TicketMessage, ...] | None
TicketDetails.__dataclass_fields__['latest_messages'].type = tuple[TicketMessage, ...] | None
setattr(TicketDetails, '__module__', 'dex_connectors.gorgias.models')


# FindCustomerByEmailInput names the email address to look up.
@dataclass(frozen=True, slots=True)
class FindCustomerByEmailInput:
    # Email is one bare address, such as jane@example.com.
    email: str = ''


setattr(FindCustomerByEmailInput, '__module__', 'dex_connectors.gorgias.models')


# CustomerMatches lists the customers whose primary email address is Email. Gorgias rejects a
# second customer with the same primary address, so it normally holds at most one.
@dataclass(frozen=True, slots=True)
class CustomerMatches:
    # Email is the address that was looked up.
    email: str = ''
    # Customers lists the matching customers; it is empty on the notFound branch.
    customers: tuple[Customer, ...] = ()
    # HasMore reports that Gorgias listed more customers for the address than Customers holds.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'customers', _replace_nil_slices(self.customers, 1))


CustomerMatches.__annotations__['customers'] = tuple[Customer, ...] | None
CustomerMatches.__dataclass_fields__['customers'].type = tuple[Customer, ...] | None
setattr(CustomerMatches, '__module__', 'dex_connectors.gorgias.models')


# TicketRequesterInput identifies the requester by email. Gorgias uses the customer with that
# primary address or, when none exists, adds a customer named Name.
@dataclass(frozen=True, slots=True)
class TicketRequesterInput:
    # Email is one bare address such as jane@example.com.
    email: str = ''
    # Name names a customer Gorgias adds; blank lets Gorgias derive one.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(TicketRequesterInput, '__module__', 'dex_connectors.gorgias.models')


# CreateTicketInput is one new ticket and the customer's first message. Zero values leave a
# field to Gorgias's defaults and the account's rules.
@dataclass(frozen=True, slots=True)
class CreateTicketInput:
    # Subject is the ticket subject, at most MaxSubjectLength characters. It is required.
    subject: str = ''
    # Description is the customer's first message as plain text. It is required. The connector
    # also sends it HTML-escaped as body_html, so markup is shown literally and line breaks are kept.
    description: str = ''
    # Requester is the customer asking for help. It is required.
    requester: TicketRequesterInput = _dataclass_field(default_factory=TicketRequesterInput)
    # Status is open or closed, or empty for Gorgias's default, open.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Priority is a Gorgias priority, or empty for Gorgias's default, normal.
    priority: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Tags are tag names set on the new ticket; Gorgias creates unknown names and rules may add more.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # AssigneeUserID assigns the ticket to a Gorgias user, or zero for none.
    assignee_user_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'assigneeUserId', 'dex_json_omit_default': True})
    # AssigneeTeamID assigns the ticket to a Gorgias team, or zero for none.
    assignee_team_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'assigneeTeamId', 'dex_json_omit_default': True})


setattr(CreateTicketInput, '__module__', 'dex_connectors.gorgias.models')


# CreateTicketOutput is the created ticket.
@dataclass(frozen=True, slots=True)
class CreateTicketOutput:
    # Ticket is the ticket Gorgias created.
    ticket: Ticket
    # WasCreatedByEarlierAttempt reports that an earlier attempt of this Step created the ticket
    # and this attempt found it by its external ID instead of sending the request again.
    was_created_by_earlier_attempt: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasCreatedByEarlierAttempt', 'dex_json_omit_default': True})


setattr(CreateTicketOutput, '__module__', 'dex_connectors.gorgias.models')


# UpdateTicketInput is one change to an existing ticket. Empty and zero fields are left
# unchanged; this release cannot unassign a user or team.
@dataclass(frozen=True, slots=True)
class UpdateTicketInput:
    # TicketID is the Gorgias ticket ID.
    ticket_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'ticketId'})
    # Status is open or closed, or empty to keep it.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Priority is the new Gorgias priority, or empty to keep it.
    priority: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # AssigneeUserID assigns a Gorgias user, or zero to keep the assignee.
    assignee_user_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'assigneeUserId', 'dex_json_omit_default': True})
    # AssigneeTeamID assigns a Gorgias team, or zero to keep the team.
    assignee_team_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'assigneeTeamId', 'dex_json_omit_default': True})
    # AddTags adds these tag names and keeps the ticket's other tags. Names compare case sensitively.
    add_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'addTags', 'dex_json_omit_default': True})
    # RemoveTags removes these tag names and keeps the ticket's other tags.
    remove_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removeTags', 'dex_json_omit_default': True})


setattr(UpdateTicketInput, '__module__', 'dex_connectors.gorgias.models')


# UpdateTicketOutput is the ticket after the change.
@dataclass(frozen=True, slots=True)
class UpdateTicketOutput:
    # Ticket is the ticket as Gorgias returned it after the last write, or as read when nothing needed writing.
    # On a failure branch that follows an applied tag change, it is the ticket as read again after the
    # failure, so it shows that change; when that read fails, it is the ticket as first read.
    ticket: Ticket
    # WasAlreadyApplied reports that the ticket already held every requested value, for example
    # because an earlier attempt of this Step wrote them, so this attempt wrote nothing.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(UpdateTicketOutput, '__module__', 'dex_connectors.gorgias.models')


# AddNoteInput is one internal note or one public email reply to add to a ticket.
@dataclass(frozen=True, slots=True)
class AddNoteInput:
    # TicketID is the Gorgias ticket ID.
    ticket_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'ticketId'})
    # Body is the plain-text note or reply. It is required. The connector also sends it
    # HTML-escaped as body_html, so markup is shown literally and line breaks are kept.
    body: str = ''
    # IsPublicReply sends the body as an email that Gorgias delivers to the customer, to the
    # sender of the customer's newest email on the ticket, through the integration that received
    # it. The reply comes from the address an earlier agent email through that integration came
    # from, or else from that email's only To or CC address other than the customer's; when
    # neither identifies the helpdesk address, nothing is sent and providerRejected is selected.
    # False adds an internal note that only agents see.
    is_public_reply: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPublicReply', 'dex_json_omit_default': True})


setattr(AddNoteInput, '__module__', 'dex_connectors.gorgias.models')


# AddNoteOutput is the added note or reply.
@dataclass(frozen=True, slots=True)
class AddNoteOutput:
    # Message is the note or reply Gorgias added. A reply's SentAt is nil until Gorgias sends it.
    message: TicketMessage = _dataclass_field(default_factory=TicketMessage)
    # WasCreatedByEarlierAttempt reports that an earlier attempt of this Step added the message
    # and this attempt found it by its external ID instead of sending the request again.
    was_created_by_earlier_attempt: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasCreatedByEarlierAttempt', 'dex_json_omit_default': True})


setattr(AddNoteOutput, '__module__', 'dex_connectors.gorgias.models')
