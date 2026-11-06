# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import format_rfc3339_nano

from .timestamps import canonical_go_timestamp

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SearchMessagesInput filters one page of messages in one folder. Every filter is optional, and filters combine.
@dataclass(frozen=True, slots=True)
class SearchMessagesInput:
    # Folder is a folder ID from the mail folder picker or a well-known name such as inbox, archive, or
    # sentitems; blank searches DefaultFolder. Subfolders are not searched.
    folder: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # From keeps messages whose From address equals this bare address, such as jane@acme.example.com.
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from', 'dex_json_omit_default': True})
    # SubjectContains keeps messages whose subject contains this text.
    subject_contains: str = _dataclass_field(default='', metadata={'dex_json_name': 'subjectContains', 'dex_json_omit_default': True})
    # ReceivedAfter keeps messages received at or after this instant, to the second.
    received_after: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'receivedAfter', 'dex_json_omit_default': True})
    # ReceivedBefore keeps messages received strictly before this instant, to the second.
    received_before: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'receivedBefore', 'dex_json_omit_default': True})
    # IsUnread keeps only messages that are not marked read.
    is_unread: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isUnread', 'dex_json_omit_default': True})
    # Limit is 1 to MaxSearchLimit messages per page; zero uses DefaultSearchLimit.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PageCursor is nextPageCursor from an earlier Result. It carries that search's filters and page
    # size, so when it is set every field except Folder is ignored.
    page_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.received_after is not None:
            timestamp = self.received_after
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'received_after'))
            object.__setattr__(self, 'received_after', canonical)
        if self.received_before is not None:
            timestamp = self.received_before
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'received_before'))
            object.__setattr__(self, 'received_before', canonical)


setattr(SearchMessagesInput, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# EmailAddress is one mailbox address with its optional display name.
@dataclass(frozen=True, slots=True)
class EmailAddress:
    # Name is the display name, such as Jane Smith, or empty.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Address is the SMTP address, such as jane@acme.example.com.
    address: str = ''


setattr(EmailAddress, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# MessageSummary is one message's header fields and state, without its body.
@dataclass(frozen=True, slots=True)
class MessageSummary:
    # ID is the message's immutable Graph ID. It stays the same when the message moves to another folder
    # of the mailbox, so later operations can use it after moveMessage.
    id: str = ''
    # ConversationID groups the messages of one thread.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId', 'dex_json_omit_default': True})
    # InternetMessageID is the Message-ID header, with its angle brackets as Graph returns them.
    internet_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'internetMessageId', 'dex_json_omit_default': True})
    # FolderID is the ID of the mail folder that holds the message.
    folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'folderId', 'dex_json_omit_default': True})
    # Subject is the subject line, cut at MaxSubjectCharacters.
    subject: str = ''
    # From is the author shown to recipients; it can differ from Sender for a delegated send.
    from_: EmailAddress | None = _dataclass_field(default=None, metadata={'dex_json_name': 'from', 'dex_json_omit_default': True})
    # Sender is the mailbox that actually sent the message.
    sender: EmailAddress | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # ReplyTo lists the Reply-To addresses; empty means replies go to From.
    reply_to: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'replyTo', 'dex_json_omit_default': True})
    # To lists at most MaxListedAddresses primary recipients.
    to: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Cc lists at most MaxListedAddresses copied recipients.
    cc: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ReceivedAt is when the mailbox received the message, in UTC.
    received_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'receivedAt'})
    # SentAt is when the message was sent, in UTC, or nil for a draft.
    sent_at: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'sentAt', 'dex_json_omit_default': True})
    # IsRead reports whether the message is marked read.
    is_read: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isRead'})
    # IsDraft reports an unsent draft.
    is_draft: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDraft', 'dex_json_omit_default': True})
    # FlagStatus is the follow-up flag.
    flag_status: str = _dataclass_field(kw_only=True, metadata={'dex_json_name': 'flagStatus'})
    # Categories lists at most MaxListedCategories category names.
    categories: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Importance is low, normal, or high.
    importance: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # HasAttachments reports attachments other than inline images, as Graph defines it.
    has_attachments: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasAttachments'})
    # Preview is the first part of the body as plain text, at most MaxPreviewCharacters.
    preview: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.received_at is not None:
            timestamp = self.received_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'received_at'))
            object.__setattr__(self, 'received_at', canonical)
        if self.sent_at is not None:
            timestamp = self.sent_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'sent_at'))
            object.__setattr__(self, 'sent_at', canonical)


setattr(MessageSummary, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# SearchMessagesOutput is one page of matching summaries, newest first by received time.
@dataclass(frozen=True, slots=True)
class SearchMessagesOutput:
    # Folder is the searched folder as requested, a well-known name or an ID.
    folder: str = ''
    # Messages lists at most the page size summaries, newest first.
    messages: tuple[MessageSummary, ...] = ()
    # HasMore reports that NextPageCursor continues the search.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})
    # NextPageCursor is the opaque continuation to pass as pageCursor, or empty on the last page.
    next_page_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'messages', _replace_nil_slices(self.messages, 1))


SearchMessagesOutput.__annotations__['messages'] = tuple[MessageSummary, ...] | None
SearchMessagesOutput.__dataclass_fields__['messages'].type = tuple[MessageSummary, ...] | None
setattr(SearchMessagesOutput, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# GetMessageInput identifies one message.
@dataclass(frozen=True, slots=True)
class GetMessageInput:
    # MessageID is the id of a searchMessages summary or of an earlier getMessage Result.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})


setattr(GetMessageInput, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# Attachment describes one attachment without its content.
@dataclass(frozen=True, slots=True)
class Attachment:
    # ID is the attachment's Graph ID.
    id: str = ''
    # Name is the file or item name.
    name: str = ''
    # ContentType is the media type, such as application/pdf, when Graph reports one.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType', 'dex_json_omit_default': True})
    # SizeBytes is the attachment size Graph reports, in bytes.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes'})
    # IsInline reports an attachment shown in the body, such as an embedded image.
    is_inline: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isInline', 'dex_json_omit_default': True})
    # Kind is file, item, or reference.
    kind: str = _dataclass_field(kw_only=True)


setattr(Attachment, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# Message is one message with its bounded plain-text body and attachment list.
@dataclass(frozen=True, slots=True)
class Message:
    # ID is the message's immutable Graph ID. It stays the same when the message moves to another folder
    # of the mailbox, so later operations can use it after moveMessage.
    id: str = ''
    # ConversationID groups the messages of one thread.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId', 'dex_json_omit_default': True})
    # InternetMessageID is the Message-ID header, with its angle brackets as Graph returns them.
    internet_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'internetMessageId', 'dex_json_omit_default': True})
    # FolderID is the ID of the mail folder that holds the message.
    folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'folderId', 'dex_json_omit_default': True})
    # Subject is the subject line, cut at MaxSubjectCharacters.
    subject: str = ''
    # From is the author shown to recipients; it can differ from Sender for a delegated send.
    from_: EmailAddress | None = _dataclass_field(default=None, metadata={'dex_json_name': 'from', 'dex_json_omit_default': True})
    # Sender is the mailbox that actually sent the message.
    sender: EmailAddress | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # ReplyTo lists the Reply-To addresses; empty means replies go to From.
    reply_to: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'replyTo', 'dex_json_omit_default': True})
    # To lists at most MaxListedAddresses primary recipients.
    to: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Cc lists at most MaxListedAddresses copied recipients.
    cc: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ReceivedAt is when the mailbox received the message, in UTC.
    received_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'receivedAt'})
    # SentAt is when the message was sent, in UTC, or nil for a draft.
    sent_at: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'sentAt', 'dex_json_omit_default': True})
    # IsRead reports whether the message is marked read.
    is_read: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isRead'})
    # IsDraft reports an unsent draft.
    is_draft: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDraft', 'dex_json_omit_default': True})
    # FlagStatus is the follow-up flag.
    flag_status: str = _dataclass_field(kw_only=True, metadata={'dex_json_name': 'flagStatus'})
    # Categories lists at most MaxListedCategories category names.
    categories: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Importance is low, normal, or high.
    importance: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # HasAttachments reports attachments other than inline images, as Graph defines it.
    has_attachments: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasAttachments'})
    # Preview is the first part of the body as plain text, at most MaxPreviewCharacters.
    preview: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Bcc lists blind-copied recipients, which Graph returns only for messages this mailbox sent.
    bcc: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Text is the body as plain text, cut at MaxTextBytes on a UTF-8 boundary.
    text: str = ''
    # IsTextTruncated reports that Text was cut.
    is_text_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isTextTruncated', 'dex_json_omit_default': True})
    # TextSource is text when Graph converted the body to plain text, or html when the connector removed
    # HTML markup itself because Graph returned HTML.
    text_source: str = _dataclass_field(default='', metadata={'dex_json_name': 'textSource'})
    # WebLink opens the message in Outlook on the web for a person reviewing it.
    web_link: str = _dataclass_field(default='', metadata={'dex_json_name': 'webLink', 'dex_json_omit_default': True})
    # Attachments lists at most MaxListedAttachments attachments without their content.
    attachments: tuple[Attachment, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # IsAttachmentListTruncated reports that the message has more attachments than were listed.
    is_attachment_list_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAttachmentListTruncated', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.received_at is not None:
            timestamp = self.received_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'received_at'))
            object.__setattr__(self, 'received_at', canonical)
        if self.sent_at is not None:
            timestamp = self.sent_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'sent_at'))
            object.__setattr__(self, 'sent_at', canonical)


setattr(Message, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# SendMessageInput is one new plain-text message.
@dataclass(frozen=True, slots=True)
class SendMessageInput:
    # To lists bare recipient addresses, such as jane@acme.example.com; at least one is required.
    to: tuple[str, ...] = ()
    # Cc lists bare addresses that receive a copy.
    cc: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Bcc lists bare addresses that receive a blind copy and appear in no header.
    bcc: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ReplyTo lists bare addresses that replies should go to; empty means the mailbox itself.
    reply_to: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'replyTo', 'dex_json_omit_default': True})
    # Subject is one line of at most MaxSubjectCharacters.
    subject: str = ''
    # Text is the plain-text body, at most MaxSendTextBytes.
    text: str = ''

    def __post_init__(self) -> None:
        object.__setattr__(self, 'to', _replace_nil_slices(self.to, 1))


SendMessageInput.__annotations__['to'] = tuple[str, ...] | None
SendMessageInput.__dataclass_fields__['to'].type = tuple[str, ...] | None
setattr(SendMessageInput, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# SentMessage identifies a message sendMessage or replyToMessage sent, or the draft an uncertain send left.
@dataclass(frozen=True, slots=True)
class SentMessage:
    # MessageID is the draft's immutable Graph ID. After sending it names the Sent Items copy, so getMessage
    # can read it; an uncertain Result names the draft to inspect.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId', 'dex_json_omit_default': True})
    # InternetMessageID is the Message-ID header Exchange assigned, when Graph returned it.
    internet_message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'internetMessageId', 'dex_json_omit_default': True})
    # ConversationID is the thread of the message.
    conversation_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conversationId', 'dex_json_omit_default': True})
    # Subject is the subject Exchange stored, such as RE: with the original subject for a reply.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Recipients lists the To, Cc, and Bcc addresses of the draft, in that order.
    recipients: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # IdempotencyMarker is the draft's IdempotencyMarkerPropertyID value, IdempotencyMarkerPrefix and the
    # Step's idempotency key.
    idempotency_marker: str = _dataclass_field(default='', metadata={'dex_json_name': 'idempotencyMarker'})
    # WasAlreadySent reports that an earlier attempt of this Step execution sent the draft and this attempt
    # confirmed it from the Sent Items copy instead of sending again.
    was_already_sent: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadySent', 'dex_json_omit_default': True})


setattr(SentMessage, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# ReplyToMessageInput is one plain-text reply to a message in the mailbox.
@dataclass(frozen=True, slots=True)
class ReplyToMessageInput:
    # MessageID is the id of the message to answer, from searchMessages or getMessage.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # Text is the reply, at most MaxSendTextBytes; Outlook places it above the quoted original.
    text: str = ''
    # IsReplyAll also addresses the original To and Cc recipients, as Outlook's Reply all does.
    is_reply_all: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isReplyAll', 'dex_json_omit_default': True})


setattr(ReplyToMessageInput, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# MoveMessageInput moves one message to another folder of the same mailbox.
@dataclass(frozen=True, slots=True)
class MoveMessageInput:
    # MessageID is the message's id from searchMessages or getMessage.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # DestinationFolder is a folder ID from the mail folder picker or a well-known name such as archive or
    # deleteditems. The folder must exist; the connector never creates folders.
    destination_folder: str = _dataclass_field(default='', metadata={'dex_json_name': 'destinationFolder'})


setattr(MoveMessageInput, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# MovedMessage is a message after the move.
@dataclass(frozen=True, slots=True)
class MovedMessage:
    # MessageID is the message's immutable ID, the same before and after the move.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # DestinationFolderID is the ID of the folder that now holds the message.
    destination_folder_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'destinationFolderId'})
    # DestinationFolderName is that folder's display name.
    destination_folder_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'destinationFolderName', 'dex_json_omit_default': True})
    # WasAlreadyMoved reports that the message was already in the folder, so nothing was moved.
    was_already_moved: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyMoved', 'dex_json_omit_default': True})


setattr(MovedMessage, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# SetMessageFlagsInput sets one message's state to absolute values; a nil field leaves that state alone.
@dataclass(frozen=True, slots=True)
class SetMessageFlagsInput:
    # MessageID is the message's id from searchMessages or getMessage.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # IsRead marks the message read when true and unread when false.
    is_read: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'isRead', 'dex_json_omit_default': True})
    # FlagStatus sets the follow-up flag to notFlagged, flagged, or complete.
    flag_status: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'flagStatus', 'dex_json_omit_default': True})
    # Categories replaces the message's categories with these names; an empty list removes every category.
    # Names compare without case, at most MaxListedCategories of at most MaxCategoryCharacters each. A name
    # missing from the mailbox's category list is still applied, without a color.
    categories: tuple[str, ...] | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(SetMessageFlagsInput, '__module__', 'dex_connectors.microsoft_outlook_mail.models')


# MessageFlags is a message's state after setMessageFlags.
@dataclass(frozen=True, slots=True)
class MessageFlags:
    # MessageID is the message's immutable ID.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # IsRead reports whether the message is marked read.
    is_read: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isRead'})
    # FlagStatus is the follow-up flag.
    flag_status: str = _dataclass_field(kw_only=True, metadata={'dex_json_name': 'flagStatus'})
    # Categories lists the message's categories.
    categories: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # WasAlreadyApplied reports that the message already held every requested value, so nothing was written.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(MessageFlags, '__module__', 'dex_connectors.microsoft_outlook_mail.models')
