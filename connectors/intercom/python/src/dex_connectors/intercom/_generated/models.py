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


# SearchConversationsInput selects conversations with typed filters that the connector turns into one
# Intercom search query. Different filters narrow each other; several states, contact IDs, or tag IDs
# match any of them. At least one filter is required.
@dataclass(frozen=True, slots=True)
class SearchConversationsInput:
    # States matches conversations in any of these Intercom states. Empty matches every state.
    states: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ContactEmail matches conversations whose first message came from this email address, Intercom's
    # source.author.email, such as jane@example.com. An admin-initiated conversation's source author is
    # the admin, so use ContactIDs to find every conversation with a contact.
    contact_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactEmail', 'dex_json_omit_default': True})
    # ContactIDs matches conversations with any of up to 15 Intercom contact IDs, as findContactByEmail returns.
    contact_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'contactIds', 'dex_json_omit_default': True})
    # TagIDs matches conversations carrying any of up to 15 Intercom tag IDs; Intercom searches tags by ID, not name.
    tag_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'tagIds', 'dex_json_omit_default': True})
    # UpdatedSince matches conversations updated at or after this RFC 3339 instant with an explicit
    # offset, such as 2026-01-21T00:00:00Z. Blank applies no time filter.
    updated_since: str = _dataclass_field(default='', metadata={'dex_json_name': 'updatedSince', 'dex_json_omit_default': True})
    # PageSize is 1 to MaxSearchPageSize conversations; zero uses DefaultSearchPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Cursor is a previous page's NextCursor.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(SearchConversationsInput, '__module__', 'dex_connectors.intercom.models')


# ConversationAuthor is the sender of a message or conversation part.
@dataclass(frozen=True, slots=True)
class ConversationAuthor:
    # Type is Intercom's author type, such as user, lead, admin, bot, or team.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ID is the author's Intercom ID: a contact ID for a user or lead, an admin ID for an admin.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Name is the author's display name, or empty.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email is the author's email address, or empty when Intercom has none.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ConversationAuthor, '__module__', 'dex_connectors.intercom.models')


# ConversationSource is the message that started a conversation.
@dataclass(frozen=True, slots=True)
class ConversationSource:
    # Type is how the conversation began, such as conversation, email, or whatsapp.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DeliveredAs is Intercom's value for how the conversation was initiated, such as operator_initiated.
    delivered_as: str = _dataclass_field(default='', metadata={'dex_json_name': 'deliveredAs', 'dex_json_omit_default': True})
    # Subject is the message subject as plain text, or empty.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Body is the first message as plain text. Only getConversation sets it.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsBodyTruncated reports that Body was cut at MaxTextBytes.
    is_body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBodyTruncated', 'dex_json_omit_default': True})
    # Author is who sent the first message: a contact for an inbound conversation, or an admin or bot.
    author: ConversationAuthor = _dataclass_field(default_factory=ConversationAuthor)


setattr(ConversationSource, '__module__', 'dex_connectors.intercom.models')


# ConversationContact references a user or lead in a conversation.
@dataclass(frozen=True, slots=True)
class ConversationContact:
    # ID is the Intercom contact ID.
    id: str = ''
    # ExternalID is the application's own ID for the contact, or empty.
    external_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'externalId', 'dex_json_omit_default': True})


setattr(ConversationContact, '__module__', 'dex_connectors.intercom.models')


# ConversationTag is a tag applied to a conversation.
@dataclass(frozen=True, slots=True)
class ConversationTag:
    # ID is the Intercom tag ID.
    id: str = ''
    # Name is the tag name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ConversationTag, '__module__', 'dex_connectors.intercom.models')


# Conversation is the connector's view of one Intercom conversation without its parts. Intercom's
# statistics, custom attributes, SLA, rating, company, AI agent, and linked objects are omitted. Empty
# assignee IDs mean the conversation is unassigned.
@dataclass(frozen=True, slots=True)
class Conversation:
    # ID is the Intercom conversation ID, a string of digits such as 215472658213.
    id: str = ''
    # Title is the conversation title, or empty when it has none.
    title: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # State is Intercom's state: open, closed, or snoozed.
    state: str = _dataclass_field(kw_only=True)
    # IsOpen is Intercom's open flag; it is true for an open or snoozed conversation.
    is_open: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOpen'})
    # IsRead is Intercom's read flag for the conversation.
    is_read: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isRead', 'dex_json_omit_default': True})
    # Priority is Intercom's priority value, such as none, low, medium, high, or urgent in API version 2.16.
    priority: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # AdminAssigneeID is the assigned admin, or empty when no admin is assigned.
    admin_assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'adminAssigneeId', 'dex_json_omit_default': True})
    # TeamAssigneeID is the assigned team, or empty when no team is assigned.
    team_assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'teamAssigneeId', 'dex_json_omit_default': True})
    # Source is the message that started the conversation. Its Body is set only by getConversation.
    source: ConversationSource = _dataclass_field(default_factory=ConversationSource)
    # Contacts lists the users and leads in the conversation; it holds one unless more were added.
    contacts: tuple[ConversationContact, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Tags lists the conversation's tags.
    tags: tuple[ConversationTag, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # CreatedAt is when the conversation was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the conversation last changed.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_format': 'rfc3339'})
    # WaitingSince is when the customer started waiting for a reply, or nil when an admin replied last.
    waiting_since: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'waitingSince', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # SnoozedUntil is when a snoozed conversation reopens, or nil.
    snoozed_until: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'snoozedUntil', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime(self.updated_at, 'updated_at'))
        elif self.updated_at is not None:
            validate_go_datetime(self.updated_at, 'updated_at')
        if isinstance(self.waiting_since, str):
            object.__setattr__(self, 'waiting_since', parse_go_datetime(self.waiting_since, 'waiting_since'))
        elif self.waiting_since is not None:
            validate_go_datetime(self.waiting_since, 'waiting_since')
        if isinstance(self.snoozed_until, str):
            object.__setattr__(self, 'snoozed_until', parse_go_datetime(self.snoozed_until, 'snoozed_until'))
        elif self.snoozed_until is not None:
            validate_go_datetime(self.snoozed_until, 'snoozed_until')


setattr(Conversation, '__module__', 'dex_connectors.intercom.models')


# SearchConversationsOutput is one page of matching conversations. Intercom's search index can lag a
# change by a short time, so a page can omit a change made moments earlier.
@dataclass(frozen=True, slots=True)
class SearchConversationsOutput:
    # Conversations lists the matching conversations without their parts or first-message body.
    conversations: tuple[Conversation, ...] = ()
    # TotalCount is Intercom's count of every matching conversation.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})
    # NextCursor reads the next page; it is empty on the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'conversations', _replace_nil_slices(self.conversations, 1))


SearchConversationsOutput.__annotations__['conversations'] = tuple[Conversation, ...] | None
SearchConversationsOutput.__dataclass_fields__['conversations'].type = tuple[Conversation, ...] | None
setattr(SearchConversationsOutput, '__module__', 'dex_connectors.intercom.models')


# GetConversationInput names one conversation and how many of its latest parts to return.
@dataclass(frozen=True, slots=True)
class GetConversationInput:
    # ConversationID is the Intercom conversation ID, a string of digits.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # LatestPartLimit is 1 to MaxLatestPartLimit; zero uses DefaultLatestPartLimit.
    latest_part_limit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'latestPartLimit', 'dex_json_omit_default': True})


setattr(GetConversationInput, '__module__', 'dex_connectors.intercom.models')


# ConversationPart is one message or event in a conversation, such as a reply, note, or close.
@dataclass(frozen=True, slots=True)
class ConversationPart:
    # ID is the Intercom conversation part ID.
    id: str = ''
    # PartType is Intercom's part type, such as comment, note, close, open, snoozed, or assignment.
    part_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'partType'})
    # Body is the part's message as plain text, or empty for an event without one.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsBodyTruncated reports that Body was cut at MaxTextBytes.
    is_body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBodyTruncated', 'dex_json_omit_default': True})
    # Author is who created the part.
    author: ConversationAuthor = _dataclass_field(default_factory=ConversationAuthor)
    # CreatedAt is when the part was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')


setattr(ConversationPart, '__module__', 'dex_connectors.intercom.models')


# ConversationDetails is one conversation with its first message and latest parts.
@dataclass(frozen=True, slots=True)
class ConversationDetails:
    # Conversation is the conversation, with Source.Body set to the first message as plain text.
    conversation: Conversation
    # LatestParts lists the newest parts first: replies, notes, and events such as close or assignment.
    latest_parts: tuple[ConversationPart, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'latestParts'})
    # HasOlderParts reports that the conversation has parts older than LatestParts.
    has_older_parts: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasOlderParts', 'dex_json_omit_default': True})
    # PartCount is Intercom's count of the conversation's parts.
    part_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'partCount'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'latest_parts', _replace_nil_slices(self.latest_parts, 1))


ConversationDetails.__annotations__['latest_parts'] = tuple[ConversationPart, ...] | None
ConversationDetails.__dataclass_fields__['latest_parts'].type = tuple[ConversationPart, ...] | None
setattr(ConversationDetails, '__module__', 'dex_connectors.intercom.models')


# ReplyToConversationInput is one admin reply or internal note to add to a conversation.
@dataclass(frozen=True, slots=True)
class ReplyToConversationInput:
    # ConversationID is the Intercom conversation ID, a string of digits.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # AdminID is the Intercom admin the reply is authored by, chosen with the adminPicker unit.
    admin_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'adminId'})
    # MessageType is comment for a reply the customer sees or note for an internal note. It is required,
    # so a customer-visible message is never sent by default.
    message_type: str = _dataclass_field(kw_only=True, metadata={'dex_json_name': 'messageType'})
    # Body is the plain-text message, at most MaxTextBytes. The connector escapes it into Intercom's HTML
    # body, so markup is shown literally, blank lines separate paragraphs, and line breaks are kept.
    body: str = ''


setattr(ReplyToConversationInput, '__module__', 'dex_connectors.intercom.models')


# ReplyToConversationOutput is the reply or note in the conversation.
@dataclass(frozen=True, slots=True)
class ReplyToConversationOutput:
    # ConversationID is the conversation the reply belongs to.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # Part is the reply part. Its ID is empty when Intercom's response did not show the new part.
    part: ConversationPart = _dataclass_field(default_factory=ConversationPart)
    # ConversationState is the conversation's state after the reply, as Intercom reported it.
    conversation_state: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'conversationState', 'dex_json_omit_default': True})
    # WasAlreadyApplied reports that an earlier attempt of this Step sent the reply without a confirmed
    # outcome and this attempt found it in the conversation, so nothing was sent again.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(ReplyToConversationOutput, '__module__', 'dex_connectors.intercom.models')


# UpdateConversationStateInput moves one conversation to an Intercom state as an admin.
@dataclass(frozen=True, slots=True)
class UpdateConversationStateInput:
    # ConversationID is the Intercom conversation ID, a string of digits.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # AdminID is the Intercom admin performing the change, chosen with the adminPicker unit.
    admin_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'adminId'})
    # State is the requested Intercom state: closed closes, open reopens a closed or snoozed
    # conversation, and snoozed hides it until SnoozedUntil.
    state: str = _dataclass_field(kw_only=True)
    # SnoozedUntil is required for snoozed and blank otherwise: an RFC 3339 instant with an explicit
    # offset, such as 2026-02-01T09:00:00Z, when Intercom reopens the conversation. Intercom stores whole seconds.
    snoozed_until: str = _dataclass_field(default='', metadata={'dex_json_name': 'snoozedUntil', 'dex_json_omit_default': True})


setattr(UpdateConversationStateInput, '__module__', 'dex_connectors.intercom.models')


# UpdateConversationStateOutput is the conversation after the change.
@dataclass(frozen=True, slots=True)
class UpdateConversationStateOutput:
    # Conversation is the conversation Intercom returned after the change, or as read when nothing needed writing.
    conversation: Conversation
    # WasAlreadyApplied reports that the conversation was already in the requested state, because of an
    # earlier attempt of this Step or another change, so this attempt wrote nothing.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(UpdateConversationStateOutput, '__module__', 'dex_connectors.intercom.models')


# FindContactByEmailInput names one email address to look up.
@dataclass(frozen=True, slots=True)
class FindContactByEmailInput:
    # Email is one bare email address, such as jane@example.com, matched with Intercom's = operator.
    email: str = ''
    # ContactLimit is 1 to MaxContactLimit contacts; zero uses DefaultContactLimit.
    contact_limit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'contactLimit', 'dex_json_omit_default': True})


setattr(FindContactByEmailInput, '__module__', 'dex_connectors.intercom.models')


# Contact is the connector's view of one Intercom user or lead. Custom attributes, location, device,
# and engagement fields are omitted.
@dataclass(frozen=True, slots=True)
class Contact:
    # ID is the Intercom contact ID.
    id: str = ''
    # Role is Intercom's contact role: user or lead.
    role: str = ''
    # Email is the contact's email address.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Name is the contact's name, or empty.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ExternalID is the application's own ID for the contact, or empty.
    external_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'externalId', 'dex_json_omit_default': True})
    # CreatedAt is when the contact was added to Intercom.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the contact last changed.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime(self.updated_at, 'updated_at'))
        elif self.updated_at is not None:
            validate_go_datetime(self.updated_at, 'updated_at')


setattr(Contact, '__module__', 'dex_connectors.intercom.models')


# FindContactByEmailOutput lists the contacts with the email address. Intercom can hold several, such
# as a lead and a user for the same person, so the Flow decides which one it means.
@dataclass(frozen=True, slots=True)
class FindContactByEmailOutput:
    # Contacts lists at most ContactLimit matching contacts.
    contacts: tuple[Contact, ...] = ()
    # HasMoreContacts reports that more contacts than Contacts have the email address.
    has_more_contacts: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMoreContacts', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'contacts', _replace_nil_slices(self.contacts, 1))


FindContactByEmailOutput.__annotations__['contacts'] = tuple[Contact, ...] | None
FindContactByEmailOutput.__dataclass_fields__['contacts'].type = tuple[Contact, ...] | None
setattr(FindContactByEmailOutput, '__module__', 'dex_connectors.intercom.models')
