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


# SearchConversationsInput selects one page of Front's conversation search. Every set field is one
# filter, and Front combines them with AND. At least one field other than the page fields is required.
@dataclass(frozen=True, slots=True)
class SearchConversationsInput:
    # Text is Front search text matched in subjects and bodies: words, which all must match, and quoted
    # phrases. It is sent unchanged, so Front also applies any filter it contains; quote user-supplied text.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # InboxID keeps conversations in one inbox, such as inb_41w25.
    inbox_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'inboxId', 'dex_json_omit_default': True})
    # TagID keeps conversations with one tag, such as tag_13o8r1.
    tag_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'tagId', 'dex_json_omit_default': True})
    # Statuses are is: filters such as open and unassigned. Blank omits trashed conversations, Front's default.
    statuses: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # RecipientEmail keeps conversations where the address is a from, to, cc, or bcc handle of any message.
    recipient_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'recipientEmail', 'dex_json_omit_default': True})
    # AssigneeID keeps conversations assigned to one teammate, such as tea_2thf.
    assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # ActivityAfter keeps conversations with a message or comment created after this time, in whole seconds.
    activity_after: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'activityAfter', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # PageSize is 1 to MaxSearchPageSize; zero requests DefaultSearchPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is a previous Result's NextPageToken; send it with the same filters.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.activity_after, str):
            object.__setattr__(self, 'activity_after', parse_go_datetime_input(self.activity_after, 'activity_after'))


setattr(SearchConversationsInput, '__module__', 'dex_connectors.front.models')


# Teammate is a Front user, bot, or visitor that is assigned, authored a message, or wrote a comment.
@dataclass(frozen=True, slots=True)
class Teammate:
    # ID is the teammate ID, such as tea_2thf.
    id: str = ''
    # Email is the teammate's email address, or empty for a bot.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # FirstName is the teammate's first name, or empty.
    first_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'firstName', 'dex_json_omit_default': True})
    # LastName is the teammate's last name, or empty.
    last_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastName', 'dex_json_omit_default': True})


setattr(Teammate, '__module__', 'dex_connectors.front.models')


# Recipient is one handle on a conversation or message, such as the customer's email address.
@dataclass(frozen=True, slots=True)
class Recipient:
    # Handle identifies the recipient on its channel, such as jane@acme.example.com or +12345678900.
    handle: str = ''
    # Role is Front's role: from, to, cc, bcc, or reply-to.
    role: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Name is the recipient's display name, or empty.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ContactID is the Front contact with this handle, such as crd_1y8sp71, or empty when none exists.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId', 'dex_json_omit_default': True})


setattr(Recipient, '__module__', 'dex_connectors.front.models')


# Tag is a tag applied to a conversation.
@dataclass(frozen=True, slots=True)
class Tag:
    # ID is the tag ID, such as tag_13o8r1; Front adds, removes, and searches tags by ID.
    id: str = ''
    # Name is the tag name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(Tag, '__module__', 'dex_connectors.front.models')


# Conversation is the connector's view of one Front conversation. Custom fields, links, and metadata are
# omitted. Times are UTC.
@dataclass(frozen=True, slots=True)
class Conversation:
    # ID is the Front conversation ID, such as cnv_55c8c149.
    id: str = ''
    # Type is Front's conversation type: conversation, discussion, or task.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Subject is the conversation subject, or empty.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Status is Front's status: unassigned, assigned, archived, or deleted.
    status: str = _dataclass_field(kw_only=True)
    # StatusID is the ticket status ID, such as sts_5x, set only when the company uses ticketing.
    status_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'statusId', 'dex_json_omit_default': True})
    # StatusCategory is the ticket status category: open, waiting, or resolved; empty without ticketing.
    status_category: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'statusCategory', 'dex_json_omit_default': True})
    # TicketIDs lists the conversation's ticket numbers, such as TICKET-1.
    ticket_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'ticketIds', 'dex_json_omit_default': True})
    # Assignee is the assigned teammate, or nil when the conversation is unassigned.
    assignee: Teammate | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Recipient is the conversation's main recipient, generally the sender of the last inbound message.
    recipient: Recipient | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Tags lists the conversation's tags.
    tags: tuple[Tag, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # IsPrivate reports a conversation in a teammate's personal inbox.
    is_private: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPrivate', 'dex_json_omit_default': True})
    # IsSnoozed reports an archived conversation with a scheduled reminder, which Front reopens later.
    is_snoozed: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSnoozed', 'dex_json_omit_default': True})
    # CreatedAt is when the conversation was created.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the conversation last changed, or nil when Front omits it.
    updated_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # WaitingSince is when the oldest unreplied message arrived, or nil when nothing waits for a reply.
    waiting_since: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'waitingSince', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # DueAt is a task conversation's due time, or nil.
    due_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'dueAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime_input(self.created_at, 'created_at'))
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime_input(self.updated_at, 'updated_at'))
        if isinstance(self.waiting_since, str):
            object.__setattr__(self, 'waiting_since', parse_go_datetime_input(self.waiting_since, 'waiting_since'))
        if isinstance(self.due_at, str):
            object.__setattr__(self, 'due_at', parse_go_datetime_input(self.due_at, 'due_at'))


setattr(Conversation, '__module__', 'dex_connectors.front.models')


# SearchConversationsOutput is one page of conversations, ordered by last activity, newest first.
@dataclass(frozen=True, slots=True)
class SearchConversationsOutput:
    # Conversations is the page; it may hold fewer than PageSize while more remain.
    conversations: tuple[Conversation, ...] = ()
    # TotalCount is Front's count of every matching conversation.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})
    # NextPageToken requests the next page, or is empty on the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})
    # Query is the Front search query the connector sent, for review.
    query: str = ''

    def __post_init__(self) -> None:
        object.__setattr__(self, 'conversations', _replace_nil_slices(self.conversations, 1))


SearchConversationsOutput.__annotations__['conversations'] = tuple[Conversation, ...] | None
SearchConversationsOutput.__dataclass_fields__['conversations'].type = tuple[Conversation, ...] | None
setattr(SearchConversationsOutput, '__module__', 'dex_connectors.front.models')


# GetConversationInput identifies one conversation and how much of its history to return.
@dataclass(frozen=True, slots=True)
class GetConversationInput:
    # ConversationID is the Front conversation ID, such as cnv_55c8c149.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # MessageLimit is 1 to MaxMessageLimit newest messages; zero reads DefaultMessageLimit.
    message_limit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'messageLimit', 'dex_json_omit_default': True})
    # CommentLimit is 1 to MaxCommentLimit newest internal comments; zero returns DefaultCommentLimit.
    comment_limit: int = _dataclass_field(default=0, metadata={'dex_json_name': 'commentLimit', 'dex_json_omit_default': True})


setattr(GetConversationInput, '__module__', 'dex_connectors.front.models')


# Message is one message in a conversation: an inbound customer message or a reply the team sent.
@dataclass(frozen=True, slots=True)
class Message:
    # ID is the message ID, such as msg_1q15qmtq.
    id: str = ''
    # Type is Front's channel type, such as email, sms, front_chat, or custom.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsInbound reports a message received from outside the company.
    is_inbound: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isInbound'})
    # IsDraft reports an unsent draft.
    is_draft: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDraft', 'dex_json_omit_default': True})
    # Subject is the message subject, or empty.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Blurb is Front's short plain-text preview of the body.
    blurb: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Body is the message body as Front stores it, HTML for email, cut at MaxBodyBytes.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsBodyTruncated reports that Body was cut at MaxBodyBytes.
    is_body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBodyTruncated', 'dex_json_omit_default': True})
    # Author is the teammate who sent an outbound message, or nil for an inbound one.
    author: Teammate | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Recipients lists the message's handles with their roles; the from handle of an inbound message is the requester.
    recipients: tuple[Recipient, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # CreatedAt is when the message was sent or received.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime_input(self.created_at, 'created_at'))


setattr(Message, '__module__', 'dex_connectors.front.models')


# Comment is one internal comment that only teammates see.
@dataclass(frozen=True, slots=True)
class Comment:
    # ID is the comment ID, such as com_1ywg3f2.
    id: str = ''
    # Body is the comment text, which can hold Markdown, cut at MaxBodyBytes.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsBodyTruncated reports that Body was cut at MaxBodyBytes.
    is_body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBodyTruncated', 'dex_json_omit_default': True})
    # Author is the teammate who wrote the comment, or nil when Front omits it.
    author: Teammate | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # IsPinned reports a comment pinned to the conversation.
    is_pinned: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPinned', 'dex_json_omit_default': True})
    # PostedAt is when the comment was posted.
    posted_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'postedAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.posted_at, str):
            object.__setattr__(self, 'posted_at', parse_go_datetime_input(self.posted_at, 'posted_at'))


setattr(Comment, '__module__', 'dex_connectors.front.models')


# ConversationDetails is one conversation with its newest messages and comments, each newest first.
@dataclass(frozen=True, slots=True)
class ConversationDetails:
    # Conversation is the conversation; it is empty on the merged branch.
    conversation: Conversation
    # Messages are the newest messages, newest first.
    messages: tuple[Message, ...] = ()
    # HasOlderMessages reports that Front holds older messages than Messages.
    has_older_messages: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasOlderMessages', 'dex_json_omit_default': True})
    # Comments are the newest internal comments, newest first.
    comments: tuple[Comment, ...] = ()
    # HasOlderComments reports that the conversation holds older comments than Comments.
    has_older_comments: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasOlderComments', 'dex_json_omit_default': True})
    # MergedIntoConversationID is the conversation that absorbed the requested one; set only on the merged branch.
    merged_into_conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'mergedIntoConversationId', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'messages', _replace_nil_slices(self.messages, 1))
        object.__setattr__(self, 'comments', _replace_nil_slices(self.comments, 1))


ConversationDetails.__annotations__['messages'] = tuple[Message, ...] | None
ConversationDetails.__dataclass_fields__['messages'].type = tuple[Message, ...] | None
ConversationDetails.__annotations__['comments'] = tuple[Comment, ...] | None
ConversationDetails.__dataclass_fields__['comments'].type = tuple[Comment, ...] | None
setattr(ConversationDetails, '__module__', 'dex_connectors.front.models')


# ReplyToConversationInput is one reply to the customer or one internal comment.
@dataclass(frozen=True, slots=True)
class ReplyToConversationInput:
    # ConversationID is the Front conversation ID, such as cnv_55c8c149.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # Text is the plain text to send, required and at most MaxReplyTextBytes. A reply sends it as text and
    # as an HTML body with one paragraph per blank-line-separated block; a comment sends it unchanged, and
    # Front renders Markdown in comments.
    text: str = ''
    # IsInternalNote adds a comment that only teammates see, through POST /conversations/{id}/comments.
    # False sends a reply that Front delivers to the conversation's recipients, through
    # POST /conversations/{id}/messages.
    is_internal_note: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isInternalNote', 'dex_json_omit_default': True})
    # AuthorID is the teammate the reply is sent on behalf of, or who writes the comment, such as tea_2thf.
    # Blank lets Front choose: a comment is then written as the API token.
    author_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorId', 'dex_json_omit_default': True})
    # ShouldArchive archives the conversation when the reply is sent. False keeps it open, unlike Front's
    # own default; a comment must leave it false.
    should_archive: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'shouldArchive', 'dex_json_omit_default': True})


setattr(ReplyToConversationInput, '__module__', 'dex_connectors.front.models')


# ConversationReply is the reply or comment replyToConversation added.
@dataclass(frozen=True, slots=True)
class ConversationReply:
    # ConversationID is the conversation the reply or comment belongs to.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # IsInternalNote reports that a comment was added rather than a reply sent.
    is_internal_note: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isInternalNote', 'dex_json_omit_default': True})
    # MessageUID is Front's message_uid for an accepted reply; GET /messages/alt:uid:{uid} reads the message
    # once Front has created it. Empty for a comment.
    message_uid: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageUid', 'dex_json_omit_default': True})
    # CommentID is the new comment's ID, such as com_1ywg3f2; empty for a reply.
    comment_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'commentId', 'dex_json_omit_default': True})
    # AuthorID is the requested author, or empty.
    author_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorId', 'dex_json_omit_default': True})


setattr(ConversationReply, '__module__', 'dex_connectors.front.models')


# UpdateConversationInput sets a conversation's status, assignee, and tags. Every field is optional, but at
# least one change is required. Unset fields are left as they are.
@dataclass(frozen=True, slots=True)
class UpdateConversationInput:
    # ConversationID is the Front conversation ID, such as cnv_55c8c149.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # Status is open, archived, or deleted, in Front's write vocabulary. It cannot be combined with StatusID.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # StatusID is a ticket status ID, such as sts_5x, for a company that uses ticketing.
    status_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'statusId', 'dex_json_omit_default': True})
    # AssigneeID assigns the conversation to one teammate, such as tea_2thf.
    assignee_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeId', 'dex_json_omit_default': True})
    # IsUnassigned removes the assignee; it cannot be combined with AssigneeID.
    is_unassigned: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isUnassigned', 'dex_json_omit_default': True})
    # AddTagIDs are tag IDs to add, such as tag_13o8r1; a tag already on the conversation is kept once.
    add_tag_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'addTagIds', 'dex_json_omit_default': True})
    # RemoveTagIDs are tag IDs to remove; a tag not on the conversation is ignored.
    remove_tag_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removeTagIds', 'dex_json_omit_default': True})


setattr(UpdateConversationInput, '__module__', 'dex_connectors.front.models')


# UpdateConversationOutput is the conversation read back after the change.
@dataclass(frozen=True, slots=True)
class UpdateConversationOutput:
    # Conversation is the conversation as Front returned it after every write.
    conversation: Conversation
    # WasAlreadyApplied reports that the conversation already held every requested value, so this attempt
    # wrote nothing; an earlier attempt of the same Step may have written them.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(UpdateConversationOutput, '__module__', 'dex_connectors.front.models')


# FindContactByEmailInput is the email handle to look up.
@dataclass(frozen=True, slots=True)
class FindContactByEmailInput:
    # Email is one bare address, such as jane@acme.example.com, sent as Front's alt:email: contact alias.
    email: str = ''


setattr(FindContactByEmailInput, '__module__', 'dex_connectors.front.models')


# ContactHandle is one way to reach a contact.
@dataclass(frozen=True, slots=True)
class ContactHandle:
    # Handle is the value, such as jane@acme.example.com.
    handle: str = ''
    # Source is Front's handle source, such as email, phone, twitter, front_chat, or custom.
    source: str = ''


setattr(ContactHandle, '__module__', 'dex_connectors.front.models')


# Contact is a person or organization the company has communicated with.
@dataclass(frozen=True, slots=True)
class Contact:
    # ID is the contact ID, such as crd_1y8sp71.
    id: str = ''
    # Name is the contact's name, or empty.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Description is the contact's description, or empty.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Handles lists every handle that reaches the contact, such as its email addresses and phone numbers.
    handles: tuple[ContactHandle, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ListNames names the contact lists the contact belongs to.
    list_names: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'listNames', 'dex_json_omit_default': True})
    # IsPrivate reports a contact that belongs to one teammate rather than to a team.
    is_private: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPrivate', 'dex_json_omit_default': True})
    # UpdatedAt is when the contact last changed, or nil when Front omits it.
    updated_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime_input(self.updated_at, 'updated_at'))


setattr(Contact, '__module__', 'dex_connectors.front.models')


# FindContactByEmailOutput holds the matching contact. Front keeps each handle on at most one contact,
# so Contacts holds one contact on the found branch and none on notFound; it is a list so a Flow can
# treat every desk's lookup alike.
@dataclass(frozen=True, slots=True)
class FindContactByEmailOutput:
    # Contacts is the matching contact.
    contacts: tuple[Contact, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'contacts', _replace_nil_slices(self.contacts, 1))


FindContactByEmailOutput.__annotations__['contacts'] = tuple[Contact, ...] | None
FindContactByEmailOutput.__dataclass_fields__['contacts'].type = tuple[Contact, ...] | None
setattr(FindContactByEmailOutput, '__module__', 'dex_connectors.front.models')
