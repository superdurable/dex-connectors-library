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


# ListInboxesInput selects one page of the account's shared inboxes.
@dataclass(frozen=True, slots=True)
class ListInboxesInput:
    # PageToken is NextPageToken from the previous page, or empty for the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})
    # PageSize is the number of inboxes per page, from 10 to 100; zero uses DefaultPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})


setattr(ListInboxesInput, '__module__', 'dex_connectors.hiver.models')


# Inbox is one Hiver shared inbox.
@dataclass(frozen=True, slots=True)
class Inbox:
    # ID is the Hiver inbox ID that every conversation operation takes.
    id: str = ''
    # DisplayName is the inbox name shown in Hiver, such as Customer Support.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName'})
    # Email is the shared mailbox address, such as support@acme.example.com.
    email: str = ''
    # ChannelType is Hiver's channel type, such as email.
    channel_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'channelType', 'dex_json_omit_default': True})
    # IsAuthorized reports that Hiver can still access the shared mailbox; false means an admin
    # must reauthorize it in Hiver before new email arrives.
    is_authorized: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAuthorized'})


setattr(Inbox, '__module__', 'dex_connectors.hiver.models')


# ListInboxesOutput is one page of shared inboxes.
@dataclass(frozen=True, slots=True)
class ListInboxesOutput:
    # Inboxes are the shared inboxes of this page in Hiver's order.
    inboxes: tuple[Inbox, ...] = ()
    # NextPageToken reads the next page, or is empty after the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'inboxes', _replace_nil_slices(self.inboxes, 1))


ListInboxesOutput.__annotations__['inboxes'] = tuple[Inbox, ...] | None
ListInboxesOutput.__dataclass_fields__['inboxes'].type = tuple[Inbox, ...] | None
setattr(ListInboxesOutput, '__module__', 'dex_connectors.hiver.models')


# ListConversationsInput selects one page of a shared inbox's conversations. Hiver documents
# no status, assignee, or date filter for this list, so a Flow filters the returned page.
@dataclass(frozen=True, slots=True)
class ListConversationsInput:
    # InboxID is the Hiver shared inbox ID, as listInboxes returns it.
    inbox_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'inboxId'})
    # PageToken is NextPageToken from the previous page, or empty for the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})
    # PageSize is the number of conversations per page, from 10 to 100; zero uses DefaultPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})


setattr(ListConversationsInput, '__module__', 'dex_connectors.hiver.models')


# ConversationAssignee is the owner of a conversation as Hiver reports it.
@dataclass(frozen=True, slots=True)
class ConversationAssignee:
    # Type is Hiver's assignee type, such as user.
    type: str = ''
    # ID is the Hiver user ID, or the ID of another assignee type.
    id: str = ''


setattr(ConversationAssignee, '__module__', 'dex_connectors.hiver.models')


# Conversation is one Hiver conversation: a customer email thread in a shared inbox.
@dataclass(frozen=True, slots=True)
class Conversation:
    # ID is the Hiver conversation ID.
    id: str = ''
    # InboxID is the shared inbox the conversation was read from.
    inbox_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'inboxId'})
    # Status is Hiver's status: open, pending, or closed, or another value Hiver returns unchanged.
    status: str = _dataclass_field(kw_only=True)
    # Assignee is the Hiver user or team the conversation is assigned to, or nil when unassigned.
    assignee: ConversationAssignee | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # TagIDs are the IDs of the inbox tags on the conversation.
    tag_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'tagIds'})
    # GmailThreadID is the Gmail thread ID of the shared mailbox's own user, which every
    # conversation operation also accepts in place of ID.
    gmail_thread_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'gmailThreadId', 'dex_json_omit_default': True})
    # PrivatePermalink opens the conversation in Hiver for a signed-in Hiver user. Hiver's public
    # permalink, which anyone with the link can open, is deliberately not carried.
    private_permalink: str = _dataclass_field(default='', metadata={'dex_json_name': 'privatePermalink', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tag_ids', _replace_nil_slices(self.tag_ids, 1))


Conversation.__annotations__['tag_ids'] = tuple[str, ...] | None
Conversation.__dataclass_fields__['tag_ids'].type = tuple[str, ...] | None
setattr(Conversation, '__module__', 'dex_connectors.hiver.models')


# ListConversationsOutput is one page of conversations.
@dataclass(frozen=True, slots=True)
class ListConversationsOutput:
    # Conversations are the conversations of this page in Hiver's order, without message IDs.
    conversations: tuple[Conversation, ...] = ()
    # NextPageToken reads the next page, or is empty after the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'conversations', _replace_nil_slices(self.conversations, 1))


ListConversationsOutput.__annotations__['conversations'] = tuple[Conversation, ...] | None
ListConversationsOutput.__dataclass_fields__['conversations'].type = tuple[Conversation, ...] | None
setattr(ListConversationsOutput, '__module__', 'dex_connectors.hiver.models')


# GetConversationInput identifies one conversation of a shared inbox.
@dataclass(frozen=True, slots=True)
class GetConversationInput:
    # InboxID is the Hiver shared inbox ID, as listInboxes returns it.
    inbox_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'inboxId'})
    # ConversationID is the Hiver conversation ID or the shared mailbox user's Gmail thread ID.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})


setattr(GetConversationInput, '__module__', 'dex_connectors.hiver.models')


# ConversationMessage identifies one email of a conversation.
@dataclass(frozen=True, slots=True)
class ConversationMessage:
    # HiverMessageID is Hiver's message ID, which createSharedDraft accepts.
    hiver_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'hiverMessageId', 'dex_json_omit_default': True})
    # GmailMessageID is the Gmail message ID of the shared mailbox's own user.
    gmail_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'gmailMessageId', 'dex_json_omit_default': True})


setattr(ConversationMessage, '__module__', 'dex_connectors.hiver.models')


# ConversationDetails is one conversation with the IDs of its messages.
@dataclass(frozen=True, slots=True)
class ConversationDetails:
    # Conversation is the conversation Hiver returned.
    conversation: Conversation
    # Messages are the conversation's message IDs in the order Hiver returns them; Hiver does
    # not document that order.
    messages: tuple[ConversationMessage, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'messages', _replace_nil_slices(self.messages, 1))


ConversationDetails.__annotations__['messages'] = tuple[ConversationMessage, ...] | None
ConversationDetails.__dataclass_fields__['messages'].type = tuple[ConversationMessage, ...] | None
setattr(ConversationDetails, '__module__', 'dex_connectors.hiver.models')


# UpdateConversationInput is one absolute change to a conversation. Empty fields are left
# unchanged; Hiver's API documents no way to unassign a conversation.
@dataclass(frozen=True, slots=True)
class UpdateConversationInput:
    # InboxID is the Hiver shared inbox ID, as listInboxes returns it.
    inbox_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'inboxId'})
    # ConversationID is the Hiver conversation ID or the shared mailbox user's Gmail thread ID.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # Status is the new Hiver status, or empty to keep it.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # AssigneeEmail assigns the conversation to the inbox user with this email, or is empty to
    # keep the assignee. The connector first finds the user with GET /users/search, so an email
    # that is not a user of the inbox selects providerRejected before any change.
    assignee_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeEmail', 'dex_json_omit_default': True})
    # ApplyTagNames are names of existing inbox tags to add; other tags stay. The connector
    # matches a name exactly, then without regard to letter case; it never creates a tag.
    apply_tag_names: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'applyTagNames', 'dex_json_omit_default': True})
    # RemoveTagNames are names of existing inbox tags to remove; other tags stay.
    remove_tag_names: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removeTagNames', 'dex_json_omit_default': True})


setattr(UpdateConversationInput, '__module__', 'dex_connectors.hiver.models')


# Tag is one tag of a Hiver shared inbox.
@dataclass(frozen=True, slots=True)
class Tag:
    # ID is the Hiver tag ID that conversations list in TagIDs.
    id: str = ''
    # Name is the tag name shown in Hiver.
    name: str = ''


setattr(Tag, '__module__', 'dex_connectors.hiver.models')


# UpdateConversationOutput is the conversation as read back after the change.
@dataclass(frozen=True, slots=True)
class UpdateConversationOutput:
    # Conversation is the conversation Hiver returned after the change.
    conversation: Conversation
    # AssigneeUserID is the Hiver user ID that AssigneeEmail resolved to.
    assignee_user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'assigneeUserId', 'dex_json_omit_default': True})
    # AppliedTags are the inbox tags ApplyTagNames resolved to.
    applied_tags: tuple[Tag, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'appliedTags', 'dex_json_omit_default': True})
    # RemovedTags are the inbox tags RemoveTagNames resolved to.
    removed_tags: tuple[Tag, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removedTags', 'dex_json_omit_default': True})


setattr(UpdateConversationOutput, '__module__', 'dex_connectors.hiver.models')


# AddNoteInput is one internal note to add to a conversation.
@dataclass(frozen=True, slots=True)
class AddNoteInput:
    # InboxID is the Hiver shared inbox ID, as listInboxes returns it.
    inbox_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'inboxId'})
    # ConversationID is the Hiver conversation ID or the shared mailbox user's Gmail thread ID.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId'})
    # Content is the note body: plain text, emoji, or the safe subset of HTML Hiver renders.
    # Only Hiver users see notes; the customer never receives them.
    content: str = ''


setattr(AddNoteInput, '__module__', 'dex_connectors.hiver.models')


# Note is one internal note on a conversation.
@dataclass(frozen=True, slots=True)
class Note:
    # ID is the Hiver note ID.
    id: str = ''
    # ConversationID is the Hiver conversation ID Hiver reported for the note.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId', 'dex_json_omit_default': True})
    # AuthorID is the Hiver user the API key acts as.
    author_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorId', 'dex_json_omit_default': True})
    # AuthorEmail is that user's email address.
    author_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'authorEmail', 'dex_json_omit_default': True})
    # CreatedAt is when Hiver created the note, in UTC.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})


setattr(Note, '__module__', 'dex_connectors.hiver.models')


# AddNoteOutput is the note Hiver added.
@dataclass(frozen=True, slots=True)
class AddNoteOutput:
    # Note is the note Hiver returned.
    note: Note = _dataclass_field(default_factory=Note)


setattr(AddNoteOutput, '__module__', 'dex_connectors.hiver.models')


# CreateSharedDraftInput is one reply draft to one message of a conversation. Exactly one of
# HiverMessageID and GmailMessageID is required.
@dataclass(frozen=True, slots=True)
class CreateSharedDraftInput:
    # InboxID is the Hiver shared inbox ID, as listInboxes returns it.
    inbox_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'inboxId'})
    # HiverMessageID is the Hiver message ID to reply to, as getConversation returns it.
    hiver_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'hiverMessageId', 'dex_json_omit_default': True})
    # GmailMessageID is the shared mailbox user's Gmail message ID to reply to, as
    # getConversation returns it, or the message's SMTP Message-ID header such as
    # <abc123@mail.gmail.com>, which Hiver accepts as a fallback.
    gmail_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'gmailMessageId', 'dex_json_omit_default': True})
    # Body is the reply text Hiver places in the draft. Hiver's documentation does not say
    # whether it renders markup, so send plain text unless a test in your account shows otherwise.
    body: str = ''


setattr(CreateSharedDraftInput, '__module__', 'dex_connectors.hiver.models')


# SharedDraft is a reply draft that Hiver users can review, edit, and send from the conversation.
@dataclass(frozen=True, slots=True)
class SharedDraft:
    # ID is the Hiver shared draft ID.
    id: str = ''
    # ConversationID is the Hiver conversation the draft belongs to.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId', 'dex_json_omit_default': True})
    # ReplyToHiverMessageID is the Hiver message the draft replies to.
    reply_to_hiver_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'replyToHiverMessageId', 'dex_json_omit_default': True})


setattr(SharedDraft, '__module__', 'dex_connectors.hiver.models')
