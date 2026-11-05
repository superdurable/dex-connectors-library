# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk.timestamps import parse_go_datetime_input

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SearchConversationsInput selects one page of the brand's conversations with Re:amaze's list
# filters, GET /conversations. Different filters narrow each other.
@dataclass(frozen=True, slots=True)
class SearchConversationsInput:
    # Filter is Re:amaze's view: blank for every unarchived conversation, open, unassigned,
    # archived, or all.
    filter: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # RequesterEmail keeps conversations relevant to the user with this email, Re:amaze's for
    # parameter; for a customer, the conversations that customer can see, which can include
    # conversations the customer was copied on. Blank keeps every user.
    requester_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'requesterEmail', 'dex_json_omit_default': True})
    # Tags keeps conversations matching these tags, sent comma-separated as Re:amaze's tag parameter.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Channel keeps conversations in the channel with this slug, Re:amaze's category, such as support.
    channel: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Sort orders the page: blank by creation, updated by the latest customer update, or changed by
    # any update or status change.
    sort: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # CustomerMessageSince keeps conversations whose latest customer message is at or after this
    # RFC 3339 instant, Re:amaze's start_date. Blank applies no lower bound.
    customer_message_since: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerMessageSince', 'dex_json_omit_default': True})
    # CustomerMessageUntil keeps conversations whose latest customer message is at or before this
    # RFC 3339 instant, Re:amaze's end_date. Blank applies no upper bound.
    customer_message_until: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerMessageUntil', 'dex_json_omit_default': True})
    # Page is 1 to MaxSearchPage; zero reads the first page.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(SearchConversationsInput, '__module__', 'dex_connectors.reamaze.models')


# ConversationChannel is the Re:amaze channel, which Re:amaze calls a category.
@dataclass(frozen=True, slots=True)
class ConversationChannel:
    # Slug identifies the channel in API requests, such as support.
    slug: str = ''
    # Name is the channel's display name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Type is Re:amaze's integer channel type, such as 1 for email.
    type: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ConversationChannel, '__module__', 'dex_connectors.reamaze.models')


# ConversationParticipant is a customer or staff user as Re:amaze embeds them.
@dataclass(frozen=True, slots=True)
class ConversationParticipant:
    # Name is the user's name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email is the user's email address, or empty for a contact without one, such as an SMS customer.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ConversationParticipant, '__module__', 'dex_connectors.reamaze.models')


# Conversation is the connector's view of one Re:amaze conversation. Custom data attributes,
# followers, and attachments are omitted.
@dataclass(frozen=True, slots=True)
class Conversation:
    # ID is the conversation's slug, which Re:amaze documents as its unique identifier and uses
    # in every conversation URL, such as knock-knock.
    id: str = ''
    # Subject is the conversation subject.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Status is Re:amaze's integer status.
    status: int = _dataclass_field(kw_only=True)
    # Tags lists the conversation's tags, Re:amaze's tag_list.
    tags: tuple[str, ...] = ()
    # Channel is the channel the conversation belongs to, which Re:amaze calls its category.
    channel: ConversationChannel = _dataclass_field(default_factory=ConversationChannel)
    # Requester is the customer who started the conversation, Re:amaze's author, or nil when absent.
    requester: ConversationParticipant | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Assignee is the assigned staff user, or nil when the conversation is unassigned.
    assignee: ConversationParticipant | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # FirstMessage is the conversation's first message as plain or Markdown text. searchConversations
    # leaves it empty to keep pages small.
    first_message: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstMessage', 'dex_json_omit_default': True})
    # IsFirstMessageTruncated reports that FirstMessage was cut at MaxTextBytes.
    is_first_message_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFirstMessageTruncated', 'dex_json_omit_default': True})
    # CreatedAt is when the conversation was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # LastCustomerMessageAt is when the customer last wrote, or nil. It is the clock for a
    # first- or next-response target, because Re:amaze exposes no SLA due time.
    last_customer_message_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'lastCustomerMessageAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # LastStaffMessageAt is when staff last wrote, or nil. Re:amaze documents it on single
    # conversation reads, so searchConversations usually leaves it nil.
    last_staff_message_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'lastStaffMessageAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime_input(self.created_at, 'created_at'))
        if isinstance(self.last_customer_message_at, str):
            object.__setattr__(self, 'last_customer_message_at', parse_go_datetime_input(self.last_customer_message_at, 'last_customer_message_at'))
        if isinstance(self.last_staff_message_at, str):
            object.__setattr__(self, 'last_staff_message_at', parse_go_datetime_input(self.last_staff_message_at, 'last_staff_message_at'))
        object.__setattr__(self, 'tags', _replace_nil_slices(self.tags, 1))


Conversation.__annotations__['tags'] = tuple[str, ...] | None
Conversation.__dataclass_fields__['tags'].type = tuple[str, ...] | None
setattr(Conversation, '__module__', 'dex_connectors.reamaze.models')


# SearchConversationsOutput is one page of matching conversations.
@dataclass(frozen=True, slots=True)
class SearchConversationsOutput:
    # Conversations lists the page's conversations without FirstMessage.
    conversations: tuple[Conversation, ...] = ()
    # TotalCount is Re:amaze's count of every matching conversation.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})
    # PageCount is Re:amaze's number of pages.
    page_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageCount'})
    # NextPage is the page to read next, or zero after the last page.
    next_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextPage', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'conversations', _replace_nil_slices(self.conversations, 1))


SearchConversationsOutput.__annotations__['conversations'] = tuple[Conversation, ...] | None
SearchConversationsOutput.__dataclass_fields__['conversations'].type = tuple[Conversation, ...] | None
setattr(SearchConversationsOutput, '__module__', 'dex_connectors.reamaze.models')


# GetConversationInput names one conversation and how many of its newest messages to return.
@dataclass(frozen=True, slots=True)
class GetConversationInput:
    # ConversationID is the conversation's slug, such as knock-knock, as Conversation.ID reports it.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # MessageLimit is 1 to MaxMessageLimit; zero uses DefaultMessageLimit.
    message_limit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'messageLimit', 'dex_json_omit_default': True})


setattr(GetConversationInput, '__module__', 'dex_connectors.reamaze.models')


# ConversationMessage is one message or internal note in a conversation.
@dataclass(frozen=True, slots=True)
class ConversationMessage:
    # Body is the message as Re:amaze returns it, plain or Markdown text.
    body: str = ''
    # IsBodyTruncated reports that Body was cut at MaxTextBytes.
    is_body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBodyTruncated', 'dex_json_omit_default': True})
    # Visibility is Re:amaze's integer visibility.
    visibility: int = _dataclass_field(kw_only=True)
    # IsInternalNote is true for an internal note, visibility 1, that only staff see.
    is_internal_note: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isInternalNote'})
    # Origin is Re:amaze's integer origin, such as 1 for email or 7 for the API.
    origin: int = 0
    # OriginID is the message's unique origin identifier, such as the one replyToConversation sets.
    origin_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'originId', 'dex_json_omit_default': True})
    # Author is the staff user or customer who wrote the message, or nil when absent.
    author: ConversationParticipant | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # CreatedAt is when the message was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime_input(self.created_at, 'created_at'))


setattr(ConversationMessage, '__module__', 'dex_connectors.reamaze.models')


# ConversationDetails is one conversation with its newest messages and internal notes.
@dataclass(frozen=True, slots=True)
class ConversationDetails:
    # Conversation is the conversation, including its first message.
    conversation: Conversation
    # Messages lists the conversation's newest messages and internal notes, newest first, as
    # Re:amaze orders them.
    messages: tuple[ConversationMessage, ...] = ()
    # HasMoreMessages reports that the conversation has older messages than the last one returned.
    has_more_messages: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMoreMessages', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'messages', _replace_nil_slices(self.messages, 1))


ConversationDetails.__annotations__['messages'] = tuple[ConversationMessage, ...] | None
ConversationDetails.__dataclass_fields__['messages'].type = tuple[ConversationMessage, ...] | None
setattr(ConversationDetails, '__module__', 'dex_connectors.reamaze.models')


# ConversationRequesterInput identifies the customer by email. Re:amaze uses the contact with
# that address or adds one named Name.
@dataclass(frozen=True, slots=True)
class ConversationRequesterInput:
    # Email is one bare address such as jane@example.com.
    email: str = ''
    # Name names a contact Re:amaze adds; blank sends no name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ConversationRequesterInput, '__module__', 'dex_connectors.reamaze.models')


# CreateConversationInput is one new conversation on behalf of a customer. Zero values leave a
# field to Re:amaze's defaults and the channel's automations.
@dataclass(frozen=True, slots=True)
class CreateConversationInput:
    # Subject is the conversation subject. It is required.
    subject: str = ''
    # Message is the customer's first message, at most MaxTextBytes. It is required and sent
    # unchanged as Re:amaze's message body.
    message: str = ''
    # Channel is the slug of the channel to create the conversation in, Re:amaze's category, such
    # as support. It is required; Settings > Channels lists each channel and its slug.
    channel: str = ''
    # Requester is the customer the conversation is created for. It is required.
    requester: ConversationRequesterInput = _dataclass_field(default_factory=ConversationRequesterInput)
    # Status is a Re:amaze status, or nil for Re:amaze's default.
    status: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # HoldUntil is when an On Hold (5) conversation's reminder fires, an RFC 3339 instant. It
    # requires Status 5; blank sets no reminder time.
    hold_until: str = _dataclass_field(default='', metadata={'dex_json_name': 'holdUntil', 'dex_json_omit_default': True})
    # Tags are set on the new conversation; channel automations may add more.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # AssigneeEmail assigns the staff user with this email, or blank for Re:amaze's routing.
    assignee_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeEmail', 'dex_json_omit_default': True})
    # ShouldSuppressNotifications asks Re:amaze to send no email or integration notification for
    # the first message, Re:amaze's message suppress_notifications.
    should_suppress_notifications: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'shouldSuppressNotifications', 'dex_json_omit_default': True})


setattr(CreateConversationInput, '__module__', 'dex_connectors.reamaze.models')


# CreateConversationOutput is the created conversation.
@dataclass(frozen=True, slots=True)
class CreateConversationOutput:
    # Conversation is the conversation Re:amaze created.
    conversation: Conversation
    # WasAlreadyApplied reports that an earlier attempt of this Step created the conversation
    # without a confirmed outcome and this attempt found it, so nothing was sent again.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(CreateConversationOutput, '__module__', 'dex_connectors.reamaze.models')


# UpdateConversationInput is one change to an existing conversation. Nil and empty fields are
# left unchanged; this release cannot unassign a conversation.
@dataclass(frozen=True, slots=True)
class UpdateConversationInput:
    # ConversationID is the conversation's slug, such as knock-knock.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # Status is the new Re:amaze status, or nil to keep it. Status 0 (Open) is a real value, so
    # this field is a pointer.
    status: int | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # HoldUntil is when an On Hold (5) conversation's reminder fires, an RFC 3339 instant. It
    # requires Status 5 and is always written, because Re:amaze does not return it to compare.
    hold_until: str = _dataclass_field(default='', metadata={'dex_json_name': 'holdUntil', 'dex_json_omit_default': True})
    # AssigneeEmail assigns the staff user with this email, or blank to keep the assignee.
    assignee_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeEmail', 'dex_json_omit_default': True})
    # AddTags adds tags and keeps the conversation's other tags. A tag already present in any
    # letter case is kept as is.
    add_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'addTags', 'dex_json_omit_default': True})
    # RemoveTags removes tags in any letter case and keeps the conversation's other tags.
    remove_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removeTags', 'dex_json_omit_default': True})


setattr(UpdateConversationInput, '__module__', 'dex_connectors.reamaze.models')


# UpdateConversationOutput is the conversation after the change.
@dataclass(frozen=True, slots=True)
class UpdateConversationOutput:
    # Conversation is the conversation Re:amaze returned after the write, or as read when nothing
    # needed writing.
    conversation: Conversation
    # WasAlreadyApplied reports that the conversation already held every requested value, for
    # example because an earlier attempt of this Step wrote them, so this attempt wrote nothing.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(UpdateConversationOutput, '__module__', 'dex_connectors.reamaze.models')


# ReplyToConversationInput is one public reply or internal note on a conversation, attributed
# to the connection's staff user.
@dataclass(frozen=True, slots=True)
class ReplyToConversationInput:
    # ConversationID is the conversation's slug, such as knock-knock.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # Text is the reply or note, at most MaxTextBytes. It is required and sent unchanged as
    # Re:amaze's message body, which Re:amaze formats as Markdown.
    text: str = ''
    # IsInternalNote adds a note only staff see, Re:amaze visibility 1. False adds a public reply,
    # visibility 0, which Re:amaze sends to the customer through the conversation's channel.
    is_internal_note: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isInternalNote', 'dex_json_omit_default': True})
    # ShouldSuppressNotifications asks Re:amaze to send no email or integration notification for
    # the message, Re:amaze's suppress_notifications.
    should_suppress_notifications: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'shouldSuppressNotifications', 'dex_json_omit_default': True})
    # ShouldSuppressAutoResolve asks Re:amaze not to mark the conversation Done because a staff
    # user wrote, Re:amaze's suppress_autoresolve.
    should_suppress_auto_resolve: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'shouldSuppressAutoResolve', 'dex_json_omit_default': True})


setattr(ReplyToConversationInput, '__module__', 'dex_connectors.reamaze.models')


# ConversationReply is the message replyToConversation added.
@dataclass(frozen=True, slots=True)
class ConversationReply:
    # ConversationID is the conversation the message belongs to.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # Message is the reply or note as Re:amaze returned it. Its OriginID is the Step's dispatch key.
    message: ConversationMessage = _dataclass_field(kw_only=True)
    # WasAlreadyApplied reports that an earlier attempt of this Step added the message without a
    # confirmed outcome and this attempt found it, so nothing was sent again.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(ConversationReply, '__module__', 'dex_connectors.reamaze.models')


# FindContactByEmailInput names the email address to resolve.
@dataclass(frozen=True, slots=True)
class FindContactByEmailInput:
    # Email is one bare address such as jane@example.com. It matches without regard to letter case.
    email: str = ''


setattr(FindContactByEmailInput, '__module__', 'dex_connectors.reamaze.models')


# Contact is the connector's view of one Re:amaze contact. Contacts belong to the account and
# are shared by its brands; notes and custom data are omitted.
@dataclass(frozen=True, slots=True)
class Contact:
    # Name is the contact's name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email is the contact's email address, which Re:amaze uses as its key, such as in
    # GET /contacts/{email}/identities.
    email: str = ''
    # FriendlyName is the contact's friendly name, or empty.
    friendly_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'friendlyName', 'dex_json_omit_default': True})


setattr(Contact, '__module__', 'dex_connectors.reamaze.models')


# FindContactByEmailOutput is every contact on the first candidate page whose email equals the input.
@dataclass(frozen=True, slots=True)
class FindContactByEmailOutput:
    # Contacts lists the matching contacts in Re:amaze's order; Re:amaze keys a contact by email,
    # so there is usually one.
    contacts: tuple[Contact, ...] = ()
    # HasMoreCandidates reports that Re:amaze's name-or-email search listed more pages than the one
    # read, so a matching contact may exist beyond it.
    has_more_candidates: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMoreCandidates', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'contacts', _replace_nil_slices(self.contacts, 1))


FindContactByEmailOutput.__annotations__['contacts'] = tuple[Contact, ...] | None
FindContactByEmailOutput.__dataclass_fields__['contacts'].type = tuple[Contact, ...] | None
setattr(FindContactByEmailOutput, '__module__', 'dex_connectors.reamaze.models')
