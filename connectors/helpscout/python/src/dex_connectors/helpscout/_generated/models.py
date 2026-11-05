# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import RFC3339NanoTimestamp, format_rfc3339_nano

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SearchConversationsInput filters Help Scout conversations. Filters narrow each other, and a blank or
# zero filter applies no restriction, so an empty input lists active conversations in every inbox the
# app's Help Scout user can access.
@dataclass(frozen=True, slots=True)
class SearchConversationsInput:
    # MailboxID limits the search to one inbox, such as the mailboxPicker unit's mailboxId; zero searches
    # every inbox.
    mailbox_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'mailboxId', 'dex_json_omit_default': True})
    # Status is active, pending, closed, spam, or all. Blank lists active conversations, Help Scout's default.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Tag limits the search to conversations carrying this tag name.
    tag: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # CustomerEmail matches conversations where the address is in To, Cc, or Bcc or is one of a customer
    # profile's emails, as Help Scout's email search documents, such as jane@example.com.
    customer_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerEmail', 'dex_json_omit_default': True})
    # ModifiedSince matches conversations modified after this RFC 3339 instant with an explicit offset,
    # such as 2026-01-21T00:00:00Z. Help Scout compares whole seconds. Blank applies no time filter.
    modified_since: str = _dataclass_field(default='', metadata={'dex_json_name': 'modifiedSince', 'dex_json_omit_default': True})
    # Page is the 1-based page to read, at most MaxSearchPage; zero reads page 1. Pass a previous
    # result's NextPage to continue.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(SearchConversationsInput, '__module__', 'dex_connectors.helpscout.models')


# Person is a Help Scout user, team, or customer as a conversation or thread names it.
@dataclass(frozen=True, slots=True)
class Person:
    # ID is the Help Scout user, team, or customer ID.
    id: int = 0
    # Type is Help Scout's person type, such as user, team, customer, or system_user for an AI agent
    # in V3 webhook payloads.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email is the person's email address when Help Scout returns one.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # FirstName is the person's first name, or a team name.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName', 'dex_json_omit_default': True})
    # LastName is the person's last name.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName', 'dex_json_omit_default': True})


setattr(Person, '__module__', 'dex_connectors.helpscout.models')


# Conversation is the connector-safe subset of one Help Scout conversation. Times are UTC.
@dataclass(frozen=True, slots=True)
class Conversation:
    # ID is the stable conversation ID used by every operation.
    id: int = 0
    # Number is the conversation number shown in the Help Scout interface.
    number: int = 0
    # Subject is the conversation subject.
    subject: str = ''
    # Status is Help Scout's status: active, pending, closed, or spam.
    status: str = _dataclass_field(kw_only=True)
    # State is published, draft, or deleted.
    state: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Type is email, chat, or phone.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # MailboxID is the inbox that holds the conversation.
    mailbox_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'mailboxId'})
    # FolderID is the inbox folder that holds the conversation.
    folder_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'folderId', 'dex_json_omit_default': True})
    # Preview is Help Scout's preview of the newest thread, cut at 1 KiB.
    preview: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ThreadCount is Help Scout's count of published threads, excluding notes.
    thread_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'threadCount'})
    # Assignee is the user or team that owns the conversation, or nil when it is unassigned.
    assignee: Person | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # PrimaryCustomer is the customer a reply is sent to, or nil when Help Scout names none.
    primary_customer: Person | None = _dataclass_field(default=None, metadata={'dex_json_name': 'primaryCustomer', 'dex_json_omit_default': True})
    # CreatedBy is the customer or user who started the conversation.
    created_by: Person | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdBy', 'dex_json_omit_default': True})
    # Tags lists the conversation's tag names.
    tags: tuple[str, ...] = ()
    # SourceType is how the conversation arrived, such as email, chat, api, or beacon.
    source_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'sourceType', 'dex_json_omit_default': True})
    # SourceVia is customer or user.
    source_via: str = _dataclass_field(default='', metadata={'dex_json_name': 'sourceVia', 'dex_json_omit_default': True})
    # CreatedAt is when the conversation was created, or zero when Help Scout omitted it.
    created_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True})
    # ClosedAt is when the conversation was closed, or zero.
    closed_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'closedAt', 'dex_json_omit_default': True})
    # UserUpdatedAt is when a user last changed the conversation, or zero.
    user_updated_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'userUpdatedAt', 'dex_json_omit_default': True})
    # CustomerWaitingSince is when the customer started waiting for an answer, or zero.
    customer_waiting_since: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'customerWaitingSince', 'dex_json_omit_default': True})
    # WebURL opens the conversation in the Help Scout interface.
    web_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'webUrl', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'created_at', canonical)
        if self.closed_at is not None:
            timestamp = self.closed_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'closed_at', canonical)
        if self.user_updated_at is not None:
            timestamp = self.user_updated_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'user_updated_at', canonical)
        if self.customer_waiting_since is not None:
            timestamp = self.customer_waiting_since
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'customer_waiting_since', canonical)
        object.__setattr__(self, 'tags', _replace_nil_slices(self.tags, 1))


Conversation.__annotations__['tags'] = tuple[str, ...] | None
Conversation.__dataclass_fields__['tags'].type = tuple[str, ...] | None
setattr(Conversation, '__module__', 'dex_connectors.helpscout.models')


# SearchConversationsOutput is one page of at most ConversationsPageSize matching conversations, newest
# created first, without threads.
@dataclass(frozen=True, slots=True)
class SearchConversationsOutput:
    # Conversations lists the page's conversations.
    conversations: tuple[Conversation, ...] = ()
    # Page is the 1-based page that was read.
    page: int = 0
    # NextPage is the page to pass for more results, or zero on the last page.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage', 'dex_json_omit_default': True})
    # TotalPages is Help Scout's page count for the search.
    total_pages: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalPages'})
    # TotalConversations is Help Scout's count of matching conversations.
    total_conversations: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalConversations'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'conversations', _replace_nil_slices(self.conversations, 1))


SearchConversationsOutput.__annotations__['conversations'] = tuple[Conversation, ...] | None
SearchConversationsOutput.__dataclass_fields__['conversations'].type = tuple[Conversation, ...] | None
setattr(SearchConversationsOutput, '__module__', 'dex_connectors.helpscout.models')


# GetConversationInput names one conversation and how many of its newest threads to return.
@dataclass(frozen=True, slots=True)
class GetConversationInput:
    # ConversationID is the Help Scout conversation ID, not the conversation number shown to users.
    conversation_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'conversationId'})
    # ThreadLimit is 1 to MaxThreadLimit newest threads; zero returns DefaultThreadLimit.
    thread_limit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'threadLimit', 'dex_json_omit_default': True})


setattr(GetConversationInput, '__module__', 'dex_connectors.helpscout.models')


# Thread is the connector-safe subset of one conversation thread. Times are UTC.
@dataclass(frozen=True, slots=True)
class Thread:
    # ID is the thread ID.
    id: int = 0
    # Type is customer for a customer's message, message for a user's reply, note for an internal note,
    # lineitem for a change of state, or chat, phone, beaconchat, forwardparent, or forwardchild.
    type: str = ''
    # Status is the conversation status this thread set.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # State is published, draft, hidden, bounced, or review.
    state: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Body is the thread's HTML body, cut at MaxThreadBodyBytes; a lineitem has none.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsBodyTruncated reports that Body was cut at MaxThreadBodyBytes.
    is_body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBodyTruncated', 'dex_json_omit_default': True})
    # CreatedBy is the user or customer who wrote the thread.
    created_by: Person | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdBy', 'dex_json_omit_default': True})
    # SourceType is how the thread arrived, such as email, api, or web.
    source_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'sourceType', 'dex_json_omit_default': True})
    # SourceVia is customer or user.
    source_via: str = _dataclass_field(default='', metadata={'dex_json_name': 'sourceVia', 'dex_json_omit_default': True})
    # CreatedAt is when the thread was created.
    created_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'createdAt'})

    def __post_init__(self) -> None:
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'created_at', canonical)


setattr(Thread, '__module__', 'dex_connectors.helpscout.models')


# ConversationDetails is one conversation with its newest threads, newest first.
@dataclass(frozen=True, slots=True)
class ConversationDetails:
    # Conversation is the conversation, empty on the merged branch.
    conversation: Conversation
    # Threads lists up to ThreadLimit of the newest threads, customer messages, replies, notes, and
    # line items alike; each Body is cut at MaxThreadBodyBytes.
    threads: tuple[Thread, ...] = ()
    # HasOlderThreads reports that the conversation has threads older than the last one returned.
    has_older_threads: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasOlderThreads', 'dex_json_omit_default': True})
    # MergedIntoConversationID names the conversation this one was merged into, on the merged branch only.
    merged_into_conversation_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'mergedIntoConversationId', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'threads', _replace_nil_slices(self.threads, 1))


ConversationDetails.__annotations__['threads'] = tuple[Thread, ...] | None
ConversationDetails.__dataclass_fields__['threads'].type = tuple[Thread, ...] | None
setattr(ConversationDetails, '__module__', 'dex_connectors.helpscout.models')


# ReplyToConversationInput is one customer reply or internal note to add to a conversation.
@dataclass(frozen=True, slots=True)
class ReplyToConversationInput:
    # ConversationID is the Help Scout conversation ID.
    conversation_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'conversationId'})
    # Text is the reply or note, sent unchanged as Help Scout's thread text; Help Scout stores thread
    # bodies as HTML. It is required and at most MaxReplyTextBytes.
    text: str = ''
    # IsInternalNote adds a note that only the team sees, through POST /v2/conversations/{id}/notes. False
    # adds a reply that Help Scout emails to the customer, through POST /v2/conversations/{id}/reply.
    is_internal_note: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isInternalNote', 'dex_json_omit_default': True})
    # CustomerID is the customer a reply is sent to. Zero reads the conversation first and uses its primary
    # customer; a note must leave it zero.
    customer_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'customerId', 'dex_json_omit_default': True})
    # Status sets the conversation's status with the thread: active, pending, closed, or spam. Blank keeps
    # Help Scout's default, under which a reply reactivates the conversation.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(ReplyToConversationInput, '__module__', 'dex_connectors.helpscout.models')


# ConversationReply is the thread replyToConversation added.
@dataclass(frozen=True, slots=True)
class ConversationReply:
    # ConversationID is the conversation the thread belongs to.
    conversation_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'conversationId'})
    # ThreadID is the confirmed thread's ID. It is zero when no thread was confirmed.
    thread_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'threadId', 'dex_json_omit_default': True})
    # IsInternalNote reports that the thread is a note rather than a customer reply.
    is_internal_note: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isInternalNote', 'dex_json_omit_default': True})
    # CustomerID is the customer a reply was sent to; zero for a note or when read-back cannot identify the customer.
    customer_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'customerId', 'dex_json_omit_default': True})


setattr(ConversationReply, '__module__', 'dex_connectors.helpscout.models')


# UpdateConversationInput is one change to an existing conversation. Blank and zero fields are left
# unchanged; at least one change is required.
@dataclass(frozen=True, slots=True)
class UpdateConversationInput:
    # ConversationID is the Help Scout conversation ID.
    conversation_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'conversationId'})
    # Status is the new status: active, pending, closed, or spam. Blank keeps the status.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # AssigneeID assigns a Help Scout user or team, which share one ID space. Zero keeps the assignee.
    assignee_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # IsUnassigned removes the assignee; it cannot be combined with AssigneeID.
    is_unassigned: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isUnassigned', 'dex_json_omit_default': True})
    # AddTags adds tags and keeps the conversation's other tags; a tag that does not exist is created.
    add_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'addTags', 'dex_json_omit_default': True})
    # RemoveTags removes tags and keeps the conversation's other tags.
    remove_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removeTags', 'dex_json_omit_default': True})


setattr(UpdateConversationInput, '__module__', 'dex_connectors.helpscout.models')


# UpdateConversationOutput is the conversation after the change, as Help Scout returns it.
@dataclass(frozen=True, slots=True)
class UpdateConversationOutput:
    # Conversation is the conversation read back after the change, or as read when nothing needed writing.
    conversation: Conversation
    # WasAlreadyApplied reports that the conversation already held every requested value, written by an
    # earlier attempt of this Step or by anyone else, so this attempt wrote nothing.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(UpdateConversationOutput, '__module__', 'dex_connectors.helpscout.models')


# FindCustomerByEmailInput names one email address to look up.
@dataclass(frozen=True, slots=True)
class FindCustomerByEmailInput:
    # Email is one bare address, such as jane@example.com.
    email: str = ''


setattr(FindCustomerByEmailInput, '__module__', 'dex_connectors.helpscout.models')


# Customer is the connector-safe subset of one Help Scout customer profile. Times are UTC.
@dataclass(frozen=True, slots=True)
class Customer:
    # ID is the customer ID, which replyToConversation accepts as CustomerID.
    id: int = 0
    # FirstName is the customer's first name.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName', 'dex_json_omit_default': True})
    # LastName is the customer's last name.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName', 'dex_json_omit_default': True})
    # Emails lists the profile's email addresses that Help Scout embedded in the response.
    emails: tuple[str, ...] = ()
    # OrganizationID is the customer's organization, or zero.
    organization_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'organizationId', 'dex_json_omit_default': True})
    # ConversationCount is Help Scout's count of the customer's conversations.
    conversation_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'conversationCount'})
    # CreatedAt is when the profile was created.
    created_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'createdAt'})
    # UpdatedAt is when the profile was last changed, or zero.
    updated_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'created_at', canonical)
        if self.updated_at is not None:
            timestamp = self.updated_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime)
                         else str(RFC3339NanoTimestamp.from_wire(timestamp)))
            object.__setattr__(self, 'updated_at', canonical)
        object.__setattr__(self, 'emails', _replace_nil_slices(self.emails, 1))


Customer.__annotations__['emails'] = tuple[str, ...] | None
Customer.__dataclass_fields__['emails'].type = tuple[str, ...] | None
setattr(Customer, '__module__', 'dex_connectors.helpscout.models')


# CustomerMatches lists every customer profile Help Scout matched, newest first. Help Scout can hold
# several profiles for one person, so the Flow decides which one it means.
@dataclass(frozen=True, slots=True)
class CustomerMatches:
    # Email is the address that was looked up.
    email: str = ''
    # Customers lists up to MaxCustomerMatches matching profiles; it is empty on the notFound branch.
    customers: tuple[Customer, ...] = ()
    # HasMore reports that Help Scout matched more profiles than Customers holds.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'customers', _replace_nil_slices(self.customers, 1))


CustomerMatches.__annotations__['customers'] = tuple[Customer, ...] | None
CustomerMatches.__dataclass_fields__['customers'].type = tuple[Customer, ...] | None
setattr(CustomerMatches, '__module__', 'dex_connectors.helpscout.models')
