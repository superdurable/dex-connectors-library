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


# SearchMessagesInput selects messages in one mailbox. Every set filter must match. Text filters use
# IMAP SEARCH, which matches a case-insensitive substring of the header; servers with full-text indexes,
# such as Dovecot with FTS, may match whole words instead. Confirm exact values, such as the sender
# address, in the Flow.
@dataclass(frozen=True, slots=True)
class SearchMessagesInput:
    # Mailbox is the mailbox to search; blank uses DefaultMailbox.
    mailbox: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # From matches the From header, such as jane@example.com or a domain such as @example.com.
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from', 'dex_json_omit_default': True})
    # To matches the To header.
    to: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SubjectContains matches the Subject header.
    subject_contains: str = _dataclass_field(default='', metadata={'dex_json_name': 'subjectContains', 'dex_json_omit_default': True})
    # SinceDate keeps messages that arrived on or after this date, written YYYY-MM-DD. IMAP compares
    # the server's internal arrival date in the server's time zone and ignores the time of day.
    since_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'sinceDate', 'dex_json_omit_default': True})
    # BeforeDate keeps messages that arrived before this date, written YYYY-MM-DD.
    before_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'beforeDate', 'dex_json_omit_default': True})
    # IsUnseen keeps only messages without the \Seen flag; false matches seen and unseen messages.
    is_unseen: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isUnseen', 'dex_json_omit_default': True})
    # OlderThanUID continues a search: only messages with a smaller UID match. Set it from the previous
    # Result's NextOlderThanUID together with UIDValidity.
    older_than_uid: int = _dataclass_field(default=0, metadata={'dex_json_name': 'olderThanUid', 'dex_json_omit_default': True})
    # UIDValidity is required with OlderThanUID; a changed UIDVALIDITY selects providerRejected.
    uid_validity: int = _dataclass_field(default=0, metadata={'dex_json_name': 'uidValidity', 'dex_json_omit_default': True})
    # Limit is the number of summaries to return, from 1 through MaxSearchLimit; zero uses DefaultSearchLimit.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(SearchMessagesInput, '__module__', 'dex_connectors.superdurable_email.models')


# MessageReference identifies one message in one IMAP mailbox. A UID names the same
# message only while the mailbox keeps the same UIDVALIDITY, so every operation that
# takes a reference checks UIDVALIDITY first and selects notFound when it changed.
@dataclass(frozen=True, slots=True)
class MessageReference:
    # Mailbox is the IMAP mailbox name, such as INBOX or Archive.
    mailbox: str = ''
    # UIDValidity is the mailbox UIDVALIDITY value that UID belongs to; it is required.
    uid_validity: int = _dataclass_field(default=0, metadata={'dex_json_name': 'uidValidity'})
    # UID is the message's IMAP unique identifier in Mailbox; it is required.
    uid: int = 0


setattr(MessageReference, '__module__', 'dex_connectors.superdurable_email.models')


# EmailAddress is one mailbox address with its optional display name.
@dataclass(frozen=True, slots=True)
class EmailAddress:
    # Name is the decoded display name, or empty.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Address is the bare address, such as jane@example.com.
    address: str = ''


setattr(EmailAddress, '__module__', 'dex_connectors.superdurable_email.models')


# MessageSummary is the envelope, flags, and size of one message, without its body.
@dataclass(frozen=True, slots=True)
class MessageSummary:
    # Reference identifies the message for getMessage, replyToMessage, moveMessage, and setFlags.
    reference: MessageReference = _dataclass_field(default_factory=MessageReference)
    # MessageID is the Message-ID header without angle brackets, or empty when the message has none.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId', 'dex_json_omit_default': True})
    # InReplyTo is the first In-Reply-To message identifier without angle brackets, or empty.
    in_reply_to: str = _dataclass_field(default='', metadata={'dex_json_name': 'inReplyTo', 'dex_json_omit_default': True})
    # From is the first From address; its Address is empty when the header is missing or malformed.
    from_: EmailAddress = _dataclass_field(default_factory=EmailAddress, metadata={'dex_json_name': 'from'})
    # ReplyTo holds the Reply-To addresses, where a reply goes. IMAP servers report From here when the
    # message has no Reply-To header, as the IMAP ENVELOPE definition requires.
    reply_to: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'replyTo', 'dex_json_omit_default': True})
    # To holds at most MaxAddressesPerField To addresses.
    to: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Cc holds at most MaxAddressesPerField Cc addresses.
    cc: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Subject is the decoded subject, cut at MaxSubjectBytes.
    subject: str = ''
    # SentAt is the Date header, or zero when it is missing or unparseable.
    sent_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'sentAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # ReceivedAt is the server's internal date, when the message arrived in this mailbox.
    received_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'receivedAt', 'dex_json_format': 'rfc3339'})
    # IsSeen reports the \Seen flag.
    is_seen: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSeen'})
    # IsFlagged reports the \Flagged flag.
    is_flagged: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFlagged'})
    # IsAnswered reports the \Answered flag.
    is_answered: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAnswered'})
    # SizeBytes is the RFC 822 size of the whole message in bytes.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes'})
    # HasAttachments reports a part with an attachment disposition or a file name.
    has_attachments: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasAttachments'})


setattr(MessageSummary, '__module__', 'dex_connectors.superdurable_email.models')


# SearchMessagesOutput is one bounded, newest-first page of matching messages.
@dataclass(frozen=True, slots=True)
class SearchMessagesOutput:
    # Mailbox is the searched mailbox.
    mailbox: str = ''
    # UIDValidity is the mailbox UIDVALIDITY that every returned UID belongs to.
    uid_validity: int = _dataclass_field(default=0, metadata={'dex_json_name': 'uidValidity'})
    # Messages holds at most Limit summaries, highest UID first; a higher UID arrived in the mailbox later.
    messages: tuple[MessageSummary, ...] = ()
    # TotalMatched is the number of messages that matched, including those not returned.
    total_matched: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalMatched'})
    # HasMore reports matches older than the last returned summary.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})
    # NextOlderThanUID is the OlderThanUID that continues the search, or zero when HasMore is false.
    next_older_than_uid: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextOlderThanUid', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'messages', _replace_nil_slices(self.messages, 1))


SearchMessagesOutput.__annotations__['messages'] = tuple[MessageSummary, ...] | None
SearchMessagesOutput.__dataclass_fields__['messages'].type = tuple[MessageSummary, ...] | None
setattr(SearchMessagesOutput, '__module__', 'dex_connectors.superdurable_email.models')


# GetMessageInput identifies the message to read.
@dataclass(frozen=True, slots=True)
class GetMessageInput:
    # Message is a reference from searchMessages; its UIDValidity must still match the mailbox.
    message: MessageReference = _dataclass_field(default_factory=MessageReference)


setattr(GetMessageInput, '__module__', 'dex_connectors.superdurable_email.models')


# Attachment describes one attachment without its content.
@dataclass(frozen=True, slots=True)
class Attachment:
    # Part is the IMAP body section number, such as 2 or 1.2.
    part: str = ''
    # FileName is the attachment's file name, or empty when the part has none.
    file_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'fileName', 'dex_json_omit_default': True})
    # MediaType is the lowercase MIME type, such as application/pdf.
    media_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'mediaType'})
    # EncodedSizeBytes is the part's size in its transfer encoding; base64 is about four thirds of the file size.
    encoded_size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'encodedSizeBytes'})
    # IsInline reports an inline disposition, such as an image shown in the message body.
    is_inline: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isInline', 'dex_json_omit_default': True})


setattr(Attachment, '__module__', 'dex_connectors.superdurable_email.models')


# Message is one message's headers, bounded text, and attachment list, without attachment content.
@dataclass(frozen=True, slots=True)
class Message:
    # Reference identifies the message for getMessage, replyToMessage, moveMessage, and setFlags.
    reference: MessageReference = _dataclass_field(default_factory=MessageReference)
    # MessageID is the Message-ID header without angle brackets, or empty when the message has none.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId', 'dex_json_omit_default': True})
    # InReplyTo is the first In-Reply-To message identifier without angle brackets, or empty.
    in_reply_to: str = _dataclass_field(default='', metadata={'dex_json_name': 'inReplyTo', 'dex_json_omit_default': True})
    # From is the first From address; its Address is empty when the header is missing or malformed.
    from_: EmailAddress = _dataclass_field(default_factory=EmailAddress, metadata={'dex_json_name': 'from'})
    # ReplyTo holds the Reply-To addresses, where a reply goes. IMAP servers report From here when the
    # message has no Reply-To header, as the IMAP ENVELOPE definition requires.
    reply_to: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'replyTo', 'dex_json_omit_default': True})
    # To holds at most MaxAddressesPerField To addresses.
    to: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Cc holds at most MaxAddressesPerField Cc addresses.
    cc: tuple[EmailAddress, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Subject is the decoded subject, cut at MaxSubjectBytes.
    subject: str = ''
    # SentAt is the Date header, or zero when it is missing or unparseable.
    sent_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'sentAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # ReceivedAt is the server's internal date, when the message arrived in this mailbox.
    received_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'receivedAt', 'dex_json_format': 'rfc3339'})
    # IsSeen reports the \Seen flag.
    is_seen: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSeen'})
    # IsFlagged reports the \Flagged flag.
    is_flagged: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFlagged'})
    # IsAnswered reports the \Answered flag.
    is_answered: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAnswered'})
    # SizeBytes is the RFC 822 size of the whole message in bytes.
    size_bytes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sizeBytes'})
    # HasAttachments reports a part with an attachment disposition or a file name.
    has_attachments: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasAttachments'})
    # References lists at most 20 References message identifiers without angle brackets, oldest first.
    references: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Text is the decoded body text, at most MaxTextBytes with line breaks written as \n.
    text: str = ''
    # TextSource names the part Text comes from.
    text_source: str = _dataclass_field(kw_only=True, metadata={'dex_json_name': 'textSource'})
    # IsTextTruncated reports that Text was cut at MaxTextBytes or the server's body was longer than the connector reads.
    is_text_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isTextTruncated', 'dex_json_omit_default': True})
    # Attachments lists at most MaxAttachments attachments in body order.
    attachments: tuple[Attachment, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # HasMoreAttachments reports attachments beyond MaxAttachments.
    has_more_attachments: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMoreAttachments', 'dex_json_omit_default': True})


setattr(Message, '__module__', 'dex_connectors.superdurable_email.models')


# SendMessageInput is one new plain-text message.
@dataclass(frozen=True, slots=True)
class SendMessageInput:
    # To lists bare ASCII recipient addresses; at least one is required.
    to: tuple[str, ...] = ()
    # Cc lists bare ASCII copy recipients.
    cc: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Bcc lists blind-copy recipients; they receive the message but appear in no header.
    bcc: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ReplyTo is an optional bare address that replies should go to instead of the sender.
    reply_to: str = _dataclass_field(default='', metadata={'dex_json_name': 'replyTo', 'dex_json_omit_default': True})
    # Subject is one line of at most MaxSubjectBytes; non-ASCII text is encoded for mail headers.
    subject: str = ''
    # Text is the UTF-8 plain-text body of at most MaxSendTextBytes. It is sent as quoted-printable.
    text: str = ''

    def __post_init__(self) -> None:
        object.__setattr__(self, 'to', _replace_nil_slices(self.to, 1))


SendMessageInput.__annotations__['to'] = tuple[str, ...] | None
SendMessageInput.__dataclass_fields__['to'].type = tuple[str, ...] | None
setattr(SendMessageInput, '__module__', 'dex_connectors.superdurable_email.models')


# SentMessage is the message an SMTP server accepted, or, on uncertain, the message that may have been sent.
@dataclass(frozen=True, slots=True)
class SentMessage:
    # MessageID is the Message-ID without angle brackets. It is derived from the Step's idempotency key, so
    # every attempt of one Step execution uses the same value, and an uncertain Result names the message to
    # look for in the recipient's or the Sent mailbox.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # From is the sender address used in the From header and the SMTP envelope.
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from'})
    # Recipients lists the envelope recipients in To, Cc, then Bcc order.
    recipients: tuple[str, ...] = ()
    # Subject is the subject that was sent, including a reply's Re: prefix.
    subject: str = ''
    # InReplyTo is the replied-to Message-ID without angle brackets, or empty for a new message.
    in_reply_to: str = _dataclass_field(default='', metadata={'dex_json_name': 'inReplyTo', 'dex_json_omit_default': True})
    # References lists the References header identifiers of a reply, oldest first.
    references: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # SentAt is the Date header of the submitted message, or zero when an earlier attempt submitted it.
    sent_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'sentAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'recipients', _replace_nil_slices(self.recipients, 1))


SentMessage.__annotations__['recipients'] = tuple[str, ...] | None
SentMessage.__dataclass_fields__['recipients'].type = tuple[str, ...] | None
setattr(SentMessage, '__module__', 'dex_connectors.superdurable_email.models')


# ReplyToMessageInput is one plain-text reply to a message in an IMAP mailbox.
@dataclass(frozen=True, slots=True)
class ReplyToMessageInput:
    # Message is a reference from searchMessages; its UIDValidity must still match the mailbox.
    message: MessageReference = _dataclass_field(default_factory=MessageReference)
    # Text is the UTF-8 plain-text reply of at most MaxSendTextBytes. The connector does not quote the
    # original message; include any quotation in Text.
    text: str = ''
    # IsReplyAll also addresses the original To and Cc recipients, except the connection's own sender address.
    is_reply_all: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isReplyAll', 'dex_json_omit_default': True})
    # Cc lists additional bare ASCII copy recipients.
    cc: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(ReplyToMessageInput, '__module__', 'dex_connectors.superdurable_email.models')


# MoveMessageInput moves one message to another mailbox.
@dataclass(frozen=True, slots=True)
class MoveMessageInput:
    # Message is a reference from searchMessages; its UIDValidity must still match the source mailbox.
    message: MessageReference = _dataclass_field(default_factory=MessageReference)
    # DestinationMailbox is an existing mailbox, such as Archive; the connector never creates one.
    destination_mailbox: str = _dataclass_field(default='', metadata={'dex_json_name': 'destinationMailbox'})
    # MessageID is the message's Message-ID without angle brackets, from searchMessages. When set, it must
    # match the message at the UID, and an attempt that finds the UID gone looks for this Message-ID in the
    # destination and selects moved instead of notFound. Blank skips both checks.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId', 'dex_json_omit_default': True})


setattr(MoveMessageInput, '__module__', 'dex_connectors.superdurable_email.models')


# MovedMessage is the message's place in the destination mailbox.
@dataclass(frozen=True, slots=True)
class MovedMessage:
    # Destination identifies the message in the destination mailbox. Its UID and UIDValidity are zero when
    # the server reports no COPYUID, which servers without UIDPLUS omit.
    destination: MessageReference = _dataclass_field(default_factory=MessageReference)
    # WasAlreadyMoved reports that an earlier attempt had moved the message, found by its Message-ID.
    was_already_moved: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyMoved', 'dex_json_omit_default': True})


setattr(MovedMessage, '__module__', 'dex_connectors.superdurable_email.models')


# SetFlagsInput sets message flags to absolute values. A nil field leaves that flag unchanged; at least
# one field is required.
@dataclass(frozen=True, slots=True)
class SetFlagsInput:
    # Message is a reference from searchMessages; its UIDValidity must still match the mailbox.
    message: MessageReference = _dataclass_field(default_factory=MessageReference)
    # IsSeen sets (true) or clears (false) \Seen, which mail clients show as read.
    is_seen: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'isSeen', 'dex_json_omit_default': True})
    # IsFlagged sets or clears \Flagged, which mail clients show as starred or flagged.
    is_flagged: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'isFlagged', 'dex_json_omit_default': True})
    # IsAnswered sets or clears \Answered, which mail clients show as replied.
    is_answered: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'isAnswered', 'dex_json_omit_default': True})


setattr(SetFlagsInput, '__module__', 'dex_connectors.superdurable_email.models')


# MessageFlags is the message's flags after the change, read back from the server.
@dataclass(frozen=True, slots=True)
class MessageFlags:
    # Message identifies the changed message.
    message: MessageReference = _dataclass_field(default_factory=MessageReference)
    # IsSeen reports \Seen.
    is_seen: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSeen'})
    # IsFlagged reports \Flagged.
    is_flagged: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFlagged'})
    # IsAnswered reports \Answered.
    is_answered: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAnswered'})
    # WasAlreadyApplied reports that the message already held the requested flags, so nothing was written.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied', 'dex_json_omit_default': True})


setattr(MessageFlags, '__module__', 'dex_connectors.superdurable_email.models')
