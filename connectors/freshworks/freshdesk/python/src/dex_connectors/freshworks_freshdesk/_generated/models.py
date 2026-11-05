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


# SearchTicketsInput selects tickets with typed filters that the connector turns into one
# Freshdesk filter query. Different filters narrow each other; several statuses, priorities,
# or tags match any of them. At least one of Statuses, Priorities, Tags, or UpdatedSinceDate is
# required, because Freshdesk's filter query needs a condition and has no requester field.
@dataclass(frozen=True, slots=True)
class SearchTicketsInput:
    # Statuses matches tickets in any of these Freshdesk statuses, including custom ones. Empty matches every status.
    statuses: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Priorities matches tickets with any of these Freshdesk priorities. Empty matches every priority.
    priorities: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Tags matches tickets carrying any of these tags.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # UpdatedSinceDate matches tickets updated on or after this UTC calendar date, written YYYY-MM-DD.
    # Freshdesk's filter query compares dates, not instants. Blank applies no time filter.
    updated_since_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'updatedSinceDate', 'dex_json_omit_default': True})
    # RequesterEmail keeps only tickets requested by the contact with this email address, such as
    # jane@example.com. The connector looks the contact up first and filters each page itself, so a
    # page can hold fewer than SearchPageSize tickets, even none, while NextPage is set. Blank keeps
    # every requester.
    requester_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'requesterEmail', 'dex_json_omit_default': True})
    # Page is 1 to MaxSearchPage; zero reads the first page.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(SearchTicketsInput, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# Ticket is the connector's view of one Freshdesk ticket. Custom fields, email addresses,
# attachments, and association details are omitted. Zero IDs mean Freshdesk reported null,
# such as an unassigned ticket.
@dataclass(frozen=True, slots=True)
class Ticket:
    # ID is the Freshdesk ticket ID.
    id: int = 0
    # Subject is the ticket subject.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Description is the ticket's plain-text content. searchTickets leaves it empty to keep pages small.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsDescriptionTruncated reports that Description was cut at MaxTextBytes.
    is_description_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDescriptionTruncated', 'dex_json_omit_default': True})
    # Status is Freshdesk's integer status.
    status: int = _dataclass_field(kw_only=True)
    # Priority is Freshdesk's integer priority.
    priority: int = _dataclass_field(kw_only=True)
    # Source is the channel that created the ticket.
    source: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Type is the account's ticket type label, such as Question or Problem, or empty for none.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # RequesterID is the contact who asked for support.
    requester_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'requesterId', 'dex_json_omit_default': True})
    # ResponderID is the assigned agent, Freshdesk's responder_id, or zero when unassigned.
    responder_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'responderId', 'dex_json_omit_default': True})
    # GroupID is the assigned group, or zero.
    group_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'groupId', 'dex_json_omit_default': True})
    # CompanyID is the requester's company for this ticket, or zero.
    company_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'companyId', 'dex_json_omit_default': True})
    # ProductID is the product the ticket belongs to, or zero.
    product_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'productId', 'dex_json_omit_default': True})
    # Tags lists the ticket's tags. Freshdesk's search and list responses may omit them, so
    # read the ticket with getTicket before relying on its tags.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # IsEscalated reports that Freshdesk escalated the ticket for any reason.
    is_escalated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isEscalated', 'dex_json_omit_default': True})
    # IsSpam reports that the ticket is marked as spam; Freshdesk rejects updates to it.
    is_spam: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSpam', 'dex_json_omit_default': True})
    # CreatedAt is when the ticket was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the ticket last changed, at Freshdesk's one-second precision.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_format': 'rfc3339'})
    # DueBy is when the ticket is due to be resolved, or nil.
    due_by: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'dueBy', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # FirstResponseDueBy is when the first response is due, or nil.
    first_response_due_by: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'firstResponseDueBy', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(Ticket, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# SearchTicketsOutput is one page of matching tickets. Freshdesk indexes new and changed tickets
# within a few minutes, so a page can omit a change made moments earlier.
@dataclass(frozen=True, slots=True)
class SearchTicketsOutput:
    # Tickets lists the matching tickets without their Description, and possibly without Tags.
    tickets: tuple[Ticket, ...] = ()
    # TotalMatched is Freshdesk's count of tickets matching the filter query, before the
    # RequesterEmail filter; Freshdesk serves only the first 300 of them.
    total_matched: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalMatched'})
    # NextPage is the page to read next, or zero when this is the last page Freshdesk serves.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tickets', _replace_nil_slices(self.tickets, 1))


SearchTicketsOutput.__annotations__['tickets'] = tuple[Ticket, ...] | None
SearchTicketsOutput.__dataclass_fields__['tickets'].type = tuple[Ticket, ...] | None
setattr(SearchTicketsOutput, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# GetTicketInput names one ticket and how many of its first conversations to return.
@dataclass(frozen=True, slots=True)
class GetTicketInput:
    # TicketID is the Freshdesk ticket ID.
    ticket_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'ticketId'})
    # ConversationLimit is 1 to MaxConversationLimit; zero uses DefaultConversationLimit.
    conversation_limit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'conversationLimit', 'dex_json_omit_default': True})


setattr(GetTicketInput, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# TicketRequester is the connector's view of the contact who requested a ticket.
@dataclass(frozen=True, slots=True)
class TicketRequester:
    # ID is the Freshdesk contact ID.
    id: int = 0
    # Name is the contact's name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email is the contact's primary email address, or empty when Freshdesk has none.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(TicketRequester, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# TicketConversation is one reply or note on a ticket.
@dataclass(frozen=True, slots=True)
class TicketConversation:
    # ID is the Freshdesk conversation ID.
    id: int = 0
    # TicketID is the ticket the conversation belongs to.
    ticket_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'ticketId', 'dex_json_omit_default': True})
    # UserID is the agent or contact who added the conversation.
    user_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'userId', 'dex_json_omit_default': True})
    # Source is the kind of conversation, such as ConversationSourceReply or ConversationSourceNote.
    source: int = _dataclass_field(kw_only=True)
    # IsPrivate is true for a private note that only agents can see.
    is_private: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPrivate'})
    # IsIncoming is true for a conversation that appears as created from outside the portal,
    # such as a requester's emailed reply.
    is_incoming: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isIncoming', 'dex_json_omit_default': True})
    # Body is the conversation as plain text.
    body: str = ''
    # IsBodyTruncated reports that Body was cut at MaxTextBytes.
    is_body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBodyTruncated', 'dex_json_omit_default': True})
    # CreatedAt is when the conversation was added.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})


setattr(TicketConversation, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# TicketDetails is one ticket with its requester and first conversations.
@dataclass(frozen=True, slots=True)
class TicketDetails:
    # Ticket is the ticket, including its plain-text description and tags.
    ticket: Ticket
    # Requester is the ticket's requester, or nil when Freshdesk did not embed one.
    requester: TicketRequester | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Conversations lists the ticket's replies and notes oldest first, as Freshdesk orders them.
    conversations: tuple[TicketConversation, ...] = ()
    # HasMoreConversations reports that the ticket has conversations after the last one returned.
    has_more_conversations: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMoreConversations', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'conversations', _replace_nil_slices(self.conversations, 1))


TicketDetails.__annotations__['conversations'] = tuple[TicketConversation, ...] | None
TicketDetails.__dataclass_fields__['conversations'].type = tuple[TicketConversation, ...] | None
setattr(TicketDetails, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# TicketRequesterInput identifies the requester by email. Freshdesk uses the contact with that
# address or, when none exists, adds a contact named Name.
@dataclass(frozen=True, slots=True)
class TicketRequesterInput:
    # Email is one bare address such as jane@example.com.
    email: str = ''
    # Name names a contact Freshdesk adds; blank lets Freshdesk derive one.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(TicketRequesterInput, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# CreateTicketInput is one new ticket. Zero values leave a field to Freshdesk's defaults, the
# account's ticket form, and its automations.
@dataclass(frozen=True, slots=True)
class CreateTicketInput:
    # Subject is the ticket subject. It is required.
    subject: str = ''
    # Description is the ticket's plain-text content. It is required. The connector escapes it
    # into Freshdesk's HTML description, so markup is shown literally and line breaks are kept.
    description: str = ''
    # Requester is the customer asking for help. It is required.
    requester: TicketRequesterInput = _dataclass_field(default_factory=TicketRequesterInput)
    # Status is a Freshdesk status, or zero for Freshdesk's default, Open (2).
    status: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Priority is a Freshdesk priority, or zero for Freshdesk's default, Low (1).
    priority: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Type is one of the account's ticket type labels, such as Question, or empty for none.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Tags are set on the new ticket; account automations may add more.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # GroupID assigns the ticket to a group, or zero for Freshdesk's default routing.
    group_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'groupId', 'dex_json_omit_default': True})
    # ResponderID assigns the ticket to an agent, Freshdesk's responder_id, or zero for none.
    responder_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'responderId', 'dex_json_omit_default': True})


setattr(CreateTicketInput, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# CreateTicketOutput is the created ticket.
@dataclass(frozen=True, slots=True)
class CreateTicketOutput:
    # Ticket is the ticket Freshdesk created.
    ticket: Ticket


setattr(CreateTicketOutput, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# UpdateTicketInput is one change to an existing ticket. Zero and empty fields are left
# unchanged; this release cannot clear an agent or group.
@dataclass(frozen=True, slots=True)
class UpdateTicketInput:
    # TicketID is the Freshdesk ticket ID.
    ticket_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'ticketId'})
    # Status is the new Freshdesk status, including a custom one, or zero to keep it.
    status: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Priority is the new Freshdesk priority, or zero to keep it.
    priority: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # GroupID assigns a group, or zero to keep the group.
    group_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'groupId', 'dex_json_omit_default': True})
    # ResponderID assigns an agent, Freshdesk's responder_id, or zero to keep the agent.
    responder_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'responderId', 'dex_json_omit_default': True})
    # AddTags adds tags and keeps the ticket's other tags. A tag already present in any letter case is kept as is.
    add_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'addTags', 'dex_json_omit_default': True})
    # RemoveTags removes tags in any letter case and keeps the ticket's other tags.
    remove_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removeTags', 'dex_json_omit_default': True})


setattr(UpdateTicketInput, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# UpdateTicketOutput is the ticket after the change.
@dataclass(frozen=True, slots=True)
class UpdateTicketOutput:
    # Ticket is the ticket Freshdesk returned after the write, or as read when nothing needed writing.
    ticket: Ticket
    # WasAlreadyApplied reports that the ticket already held every requested value, for example
    # because an earlier attempt of this Step wrote them, so this attempt wrote nothing.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(UpdateTicketOutput, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# AddNoteInput is one private note or one public reply to add to a ticket.
@dataclass(frozen=True, slots=True)
class AddNoteInput:
    # TicketID is the Freshdesk ticket ID.
    ticket_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'ticketId'})
    # Body is the plain-text note or reply. It is required. The connector escapes it into
    # Freshdesk's HTML body, so markup is shown literally and line breaks are kept.
    body: str = ''
    # IsPublicReply sends the body as a reply that Freshdesk emails to the requester, through
    # POST /api/v2/tickets/{id}/reply. False adds a private note that only agents can see.
    is_public_reply: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPublicReply', 'dex_json_omit_default': True})


setattr(AddNoteInput, '__module__', 'dex_connectors.freshworks_freshdesk.models')


# AddNoteOutput is the added note or reply.
@dataclass(frozen=True, slots=True)
class AddNoteOutput:
    # Conversation is the note or reply Freshdesk added.
    conversation: TicketConversation


setattr(AddNoteOutput, '__module__', 'dex_connectors.freshworks_freshdesk.models')
