# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Attachment, EmailAddress, GetMessageInput, Message, MessageFlags, MessageReference, MessageSummary, MoveMessageInput, MovedMessage, ReplyToMessageInput, SearchMessagesInput, SearchMessagesOutput, SendMessageInput, SentMessage, SetFlagsInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_MESSAGE_DEFINITION, INVALID_RESPONSE, MOVED, MOVE_MESSAGE_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, REPLY_TO_MESSAGE_DEFINITION, SEARCHED, SEARCH_MESSAGES_DEFINITION, SEND_MESSAGE_DEFINITION, SENT, SET_FLAGS_DEFINITION, UNCERTAIN, UPDATED

def search_messages_searched(value: SearchMessagesOutput, failure: Failure | None = None) -> Case[SearchMessagesOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_messages_searched, '__module__', "dex_connectors.superdurable_email.emailmock")

def search_messages_provider_rejected(value: SearchMessagesOutput, failure: Failure | None = None) -> Case[SearchMessagesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_messages_provider_rejected, '__module__', "dex_connectors.superdurable_email.emailmock")

def search_messages_invalid_response(value: SearchMessagesOutput, failure: Failure | None = None) -> Case[SearchMessagesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_messages_invalid_response, '__module__', "dex_connectors.superdurable_email.emailmock")

def search_messages_defect(value: SearchMessagesOutput, failure: Failure | None = None) -> Case[SearchMessagesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_messages_defect, '__module__', "dex_connectors.superdurable_email.emailmock")

def search_messages_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchMessagesOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_messages_retry, '__module__', "dex_connectors.superdurable_email.emailmock")

def search_messages_customer_inbox_pages() -> Case[SearchMessagesOutput]:
    return Case.paged(SEARCHED, (SearchMessagesOutput(mailbox='INBOX', uid_validity=42, messages=(MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=109), message_id='lookalike-109@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T10:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=108), message_id='lookalike-108@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T10:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=107), message_id='lookalike-107@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T10:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=106), message_id='lookalike-106@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T10:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=105), message_id='lookalike-105@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T10:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=104), message_id='lookalike-104@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T10:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=103), message_id='lookalike-103@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T10:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=102), message_id='lookalike-102@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T10:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=101), message_id='lookalike-101@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T10:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=100), message_id='latest@acme.example.com', from_=EmailAddress(name='Jane Smith', address='jane@acme.example.com'), reply_to=(EmailAddress(name='Jane Smith', address='jane@acme.example.com'),), subject='Refund request for order 88213', received_at=datetime.fromisoformat("2026-01-28T09:00:00Z".replace('Z', '+00:00')), size_bytes=380)), total_matched=21, has_more=True, next_older_than_uid=100), SearchMessagesOutput(mailbox='INBOX', uid_validity=42, messages=(MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=99), message_id='lookalike-99@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T08:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=98), message_id='lookalike-98@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T08:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=97), message_id='lookalike-97@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T08:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=96), message_id='lookalike-96@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T08:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=95), message_id='lookalike-95@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T08:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=94), message_id='lookalike-94@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T08:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=93), message_id='lookalike-93@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T08:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=92), message_id='lookalike-92@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T08:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=91), message_id='lookalike-91@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T08:00:00Z".replace('Z', '+00:00')), size_bytes=245), MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=90), message_id='lookalike-90@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T08:00:00Z".replace('Z', '+00:00')), size_bytes=245)), total_matched=11, has_more=True, next_older_than_uid=90), SearchMessagesOutput(mailbox='INBOX', uid_validity=42, messages=(MessageSummary(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=89), message_id='lookalike-89@acme.example.com.au', from_=EmailAddress(name='Jane Lookalike', address='jane@acme.example.com.au'), subject='Refund request', received_at=datetime.fromisoformat("2026-01-28T08:00:00Z".replace('Z', '+00:00')), size_bytes=245),), total_matched=1, has_more=False)))

setattr(search_messages_customer_inbox_pages, '__module__', "dex_connectors.superdurable_email.emailmock")

def search_messages_rejected_imap_login() -> Case[SearchMessagesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchMessagesOutput(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "searchMessages", "The IMAP login was refused."))

setattr(search_messages_rejected_imap_login, '__module__', "dex_connectors.superdurable_email.emailmock")

def get_message_found(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_message_found, '__module__', "dex_connectors.superdurable_email.emailmock")

def get_message_not_found(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_message_not_found, '__module__', "dex_connectors.superdurable_email.emailmock")

def get_message_provider_rejected(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_message_provider_rejected, '__module__', "dex_connectors.superdurable_email.emailmock")

def get_message_invalid_response(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_message_invalid_response, '__module__', "dex_connectors.superdurable_email.emailmock")

def get_message_defect(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_message_defect, '__module__', "dex_connectors.superdurable_email.emailmock")

def get_message_retry(failure: Failure, after_seconds: float = 0) -> Case[Message]:
    return Case.retry(failure, after_seconds)

setattr(get_message_retry, '__module__', "dex_connectors.superdurable_email.emailmock")

def get_message_customer_refund_request() -> Case[Message]:
    return Case.branch_result(FOUND, Message(reference=MessageReference(mailbox='INBOX', uid_validity=42, uid=100), message_id='latest@acme.example.com', from_=EmailAddress(name='Jane Smith', address='jane@acme.example.com'), reply_to=(EmailAddress(name='Jane Smith', address='jane@acme.example.com'),), subject='Refund request for order 88213', received_at=datetime.fromisoformat("2026-01-28T09:00:00Z".replace('Z', '+00:00')), size_bytes=380, references=('older@acme.example.com',), text='I was charged twice for order 88213.', text_source='plain'))

setattr(get_message_customer_refund_request, '__module__', "dex_connectors.superdurable_email.emailmock")

def get_message_missing_uid() -> Case[Message]:
    return Case.branch_result(NOT_FOUND, Message(reference=MessageReference(), from_=EmailAddress(), text_source=''), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getMessage", "The message UID no longer exists."))

setattr(get_message_missing_uid, '__module__', "dex_connectors.superdurable_email.emailmock")

def get_message_rejected_read_login() -> Case[Message]:
    return Case.branch_result(PROVIDER_REJECTED, Message(reference=MessageReference(), from_=EmailAddress(), text_source=''), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getMessage", "The IMAP login was refused."))

setattr(get_message_rejected_read_login, '__module__', "dex_connectors.superdurable_email.emailmock")

def send_message_sent(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(SENT, value, failure)

setattr(send_message_sent, '__module__', "dex_connectors.superdurable_email.emailmock")

def send_message_provider_rejected(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(send_message_provider_rejected, '__module__', "dex_connectors.superdurable_email.emailmock")

def send_message_uncertain(value: SentMessage, failure: Failure) -> Case[SentMessage]:
    return Case.uncertain(value, failure)

setattr(send_message_uncertain, '__module__', "dex_connectors.superdurable_email.emailmock")

def send_message_defect(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_message_defect, '__module__', "dex_connectors.superdurable_email.emailmock")

def send_message_retry(failure: Failure, after_seconds: float = 0) -> Case[SentMessage]:
    return Case.retry(failure, after_seconds)

setattr(send_message_retry, '__module__', "dex_connectors.superdurable_email.emailmock")

def send_message_new_customer_message() -> Case[SentMessage]:
    return Case.branch_result(SENT, SentMessage(message_id='dex-new-message@example.com', from_='support@example.com', recipients=('jane@acme.example.com',), subject='Your refund for order 88213', sent_at=datetime.fromisoformat("2026-01-28T10:05:00Z".replace('Z', '+00:00'))))

setattr(send_message_new_customer_message, '__module__', "dex_connectors.superdurable_email.emailmock")

def send_message_recipient_rejected() -> Case[SentMessage]:
    return Case.branch_result(PROVIDER_REJECTED, SentMessage(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "sendMessage", "SMTP refused the recipient; nothing was sent."))

setattr(send_message_recipient_rejected, '__module__', "dex_connectors.superdurable_email.emailmock")

def reply_to_message_sent(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(SENT, value, failure)

setattr(reply_to_message_sent, '__module__', "dex_connectors.superdurable_email.emailmock")

def reply_to_message_not_found(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(reply_to_message_not_found, '__module__', "dex_connectors.superdurable_email.emailmock")

def reply_to_message_provider_rejected(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(reply_to_message_provider_rejected, '__module__', "dex_connectors.superdurable_email.emailmock")

def reply_to_message_invalid_response(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(reply_to_message_invalid_response, '__module__', "dex_connectors.superdurable_email.emailmock")

def reply_to_message_uncertain(value: SentMessage, failure: Failure) -> Case[SentMessage]:
    return Case.uncertain(value, failure)

setattr(reply_to_message_uncertain, '__module__', "dex_connectors.superdurable_email.emailmock")

def reply_to_message_defect(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(reply_to_message_defect, '__module__', "dex_connectors.superdurable_email.emailmock")

def reply_to_message_retry(failure: Failure, after_seconds: float = 0) -> Case[SentMessage]:
    return Case.retry(failure, after_seconds)

setattr(reply_to_message_retry, '__module__', "dex_connectors.superdurable_email.emailmock")

def reply_to_message_threaded_refund_reply() -> Case[SentMessage]:
    return Case.branch_result(SENT, SentMessage(message_id='dex-refund-reply@example.com', from_='support@example.com', recipients=('jane@acme.example.com',), subject='Re: Refund request for order 88213', in_reply_to='latest@acme.example.com', references=('older@acme.example.com', 'latest@acme.example.com'), sent_at=datetime.fromisoformat("2026-01-28T10:05:00Z".replace('Z', '+00:00'))))

setattr(reply_to_message_threaded_refund_reply, '__module__', "dex_connectors.superdurable_email.emailmock")

def reply_to_message_missing_source_message() -> Case[SentMessage]:
    return Case.branch_result(NOT_FOUND, SentMessage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "replyToMessage", "The source message no longer exists."))

setattr(reply_to_message_missing_source_message, '__module__', "dex_connectors.superdurable_email.emailmock")

def reply_to_message_rejected_reply_recipient() -> Case[SentMessage]:
    return Case.branch_result(PROVIDER_REJECTED, SentMessage(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "replyToMessage", "SMTP refused the reply recipient; nothing was sent."))

setattr(reply_to_message_rejected_reply_recipient, '__module__', "dex_connectors.superdurable_email.emailmock")

def reply_to_message_missing_reply_address() -> Case[SentMessage]:
    return Case.branch_result(INVALID_RESPONSE, SentMessage(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "replyToMessage", "The source message has no usable reply address."))

setattr(reply_to_message_missing_reply_address, '__module__', "dex_connectors.superdurable_email.emailmock")

def move_message_moved(value: MovedMessage, failure: Failure | None = None) -> Case[MovedMessage]:
    return Case.branch_result(MOVED, value, failure)

setattr(move_message_moved, '__module__', "dex_connectors.superdurable_email.emailmock")

def move_message_not_found(value: MovedMessage, failure: Failure | None = None) -> Case[MovedMessage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(move_message_not_found, '__module__', "dex_connectors.superdurable_email.emailmock")

def move_message_provider_rejected(value: MovedMessage, failure: Failure | None = None) -> Case[MovedMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(move_message_provider_rejected, '__module__', "dex_connectors.superdurable_email.emailmock")

def move_message_defect(value: MovedMessage, failure: Failure | None = None) -> Case[MovedMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(move_message_defect, '__module__', "dex_connectors.superdurable_email.emailmock")

def move_message_retry(failure: Failure, after_seconds: float = 0) -> Case[MovedMessage]:
    return Case.retry(failure, after_seconds)

setattr(move_message_retry, '__module__', "dex_connectors.superdurable_email.emailmock")

def move_message_archived_customer_request() -> Case[MovedMessage]:
    return Case.branch_result(MOVED, MovedMessage(destination=MessageReference(mailbox='Archive', uid_validity=43, uid=50)))

setattr(move_message_archived_customer_request, '__module__', "dex_connectors.superdurable_email.emailmock")

def move_message_missing_source_and_archive() -> Case[MovedMessage]:
    return Case.branch_result(NOT_FOUND, MovedMessage(destination=MessageReference()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "moveMessage", "The message is in neither mailbox."))

setattr(move_message_missing_source_and_archive, '__module__', "dex_connectors.superdurable_email.emailmock")

def move_message_archive_unavailable() -> Case[MovedMessage]:
    return Case.branch_result(PROVIDER_REJECTED, MovedMessage(destination=MessageReference()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "moveMessage", "The destination mailbox does not exist."))

setattr(move_message_archive_unavailable, '__module__', "dex_connectors.superdurable_email.emailmock")

def set_flags_updated(value: MessageFlags, failure: Failure | None = None) -> Case[MessageFlags]:
    return Case.branch_result(UPDATED, value, failure)

setattr(set_flags_updated, '__module__', "dex_connectors.superdurable_email.emailmock")

def set_flags_not_found(value: MessageFlags, failure: Failure | None = None) -> Case[MessageFlags]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(set_flags_not_found, '__module__', "dex_connectors.superdurable_email.emailmock")

def set_flags_provider_rejected(value: MessageFlags, failure: Failure | None = None) -> Case[MessageFlags]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(set_flags_provider_rejected, '__module__', "dex_connectors.superdurable_email.emailmock")

def set_flags_defect(value: MessageFlags, failure: Failure | None = None) -> Case[MessageFlags]:
    return Case.branch_result(DEFECT, value, failure)

setattr(set_flags_defect, '__module__', "dex_connectors.superdurable_email.emailmock")

def set_flags_retry(failure: Failure, after_seconds: float = 0) -> Case[MessageFlags]:
    return Case.retry(failure, after_seconds)

setattr(set_flags_retry, '__module__', "dex_connectors.superdurable_email.emailmock")

def set_flags_marked_customer_request() -> Case[MessageFlags]:
    return Case.branch_result(UPDATED, MessageFlags(message=MessageReference(mailbox='INBOX', uid_validity=42, uid=100), is_seen=True, is_flagged=False, is_answered=True))

setattr(set_flags_marked_customer_request, '__module__', "dex_connectors.superdurable_email.emailmock")

def set_flags_missing_flag_target() -> Case[MessageFlags]:
    return Case.branch_result(NOT_FOUND, MessageFlags(message=MessageReference()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "setFlags", "The message UID no longer exists."))

setattr(set_flags_missing_flag_target, '__module__', "dex_connectors.superdurable_email.emailmock")

def set_flags_read_only_mailbox() -> Case[MessageFlags]:
    return Case.branch_result(PROVIDER_REJECTED, MessageFlags(message=MessageReference()), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "setFlags", "The mailbox is read-only."))

setattr(set_flags_read_only_mailbox, '__module__', "dex_connectors.superdurable_email.emailmock")


class Mock:

    def __init__(self, connection_name: str = "email") -> None:
        self.search_messages_script: Query[SearchMessagesInput, SearchMessagesOutput] = Query(SEARCH_MESSAGES_DEFINITION)
        self.search_messages_script.default(search_messages_customer_inbox_pages())
        self.get_message_script: Query[GetMessageInput, Message] = Query(GET_MESSAGE_DEFINITION)
        self.get_message_script.default(get_message_customer_refund_request())
        self.send_message_script: Mutation[SendMessageInput, SentMessage] = Mutation(SEND_MESSAGE_DEFINITION)
        self.send_message_script.default(send_message_new_customer_message())
        self.reply_to_message_script: Mutation[ReplyToMessageInput, SentMessage] = Mutation(REPLY_TO_MESSAGE_DEFINITION)
        self.reply_to_message_script.default(reply_to_message_threaded_refund_reply())
        self.move_message_script: Mutation[MoveMessageInput, MovedMessage] = Mutation(MOVE_MESSAGE_DEFINITION)
        self.move_message_script.default(move_message_archived_customer_request())
        self.set_flags_script: Mutation[SetFlagsInput, MessageFlags] = Mutation(SET_FLAGS_DEFINITION)
        self.set_flags_script.default(set_flags_marked_customer_request())
        self._connection = new_connection_with_operations(
            search_messages=self.search_messages_script,
            get_message=self.get_message_script,
            send_message=self.send_message_script,
            reply_to_message=self.reply_to_message_script,
            move_message=self.move_message_script,
            set_flags=self.set_flags_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.superdurable_email.emailmock")

__all__ = [
    "Mock",
    "search_messages_searched",
    "search_messages_provider_rejected",
    "search_messages_invalid_response",
    "search_messages_defect",
    "search_messages_retry",
    "search_messages_customer_inbox_pages",
    "search_messages_rejected_imap_login",
    "get_message_found",
    "get_message_not_found",
    "get_message_provider_rejected",
    "get_message_invalid_response",
    "get_message_defect",
    "get_message_retry",
    "get_message_customer_refund_request",
    "get_message_missing_uid",
    "get_message_rejected_read_login",
    "send_message_sent",
    "send_message_provider_rejected",
    "send_message_uncertain",
    "send_message_defect",
    "send_message_retry",
    "send_message_new_customer_message",
    "send_message_recipient_rejected",
    "reply_to_message_sent",
    "reply_to_message_not_found",
    "reply_to_message_provider_rejected",
    "reply_to_message_invalid_response",
    "reply_to_message_uncertain",
    "reply_to_message_defect",
    "reply_to_message_retry",
    "reply_to_message_threaded_refund_reply",
    "reply_to_message_missing_source_message",
    "reply_to_message_rejected_reply_recipient",
    "reply_to_message_missing_reply_address",
    "move_message_moved",
    "move_message_not_found",
    "move_message_provider_rejected",
    "move_message_defect",
    "move_message_retry",
    "move_message_archived_customer_request",
    "move_message_missing_source_and_archive",
    "move_message_archive_unavailable",
    "set_flags_updated",
    "set_flags_not_found",
    "set_flags_provider_rejected",
    "set_flags_defect",
    "set_flags_retry",
    "set_flags_marked_customer_request",
    "set_flags_missing_flag_target",
    "set_flags_read_only_mailbox",
]
