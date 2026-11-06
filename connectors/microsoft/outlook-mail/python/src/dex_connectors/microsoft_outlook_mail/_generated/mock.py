# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Attachment, EmailAddress, GetMessageInput, Message, MessageFlags, MessageSummary, MoveMessageInput, MovedMessage, ReplyToMessageInput, SearchMessagesInput, SearchMessagesOutput, SendMessageInput, SentMessage, SetMessageFlagsInput
from .definitions import CONNECTOR_ID, PROVIDER, DEFECT, FOUND, GET_MESSAGE_DEFINITION, INVALID_RESPONSE, MOVED, MOVE_MESSAGE_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, REPLY_TO_MESSAGE_DEFINITION, SEARCHED, SEARCH_MESSAGES_DEFINITION, SEND_MESSAGE_DEFINITION, SENT, SET_MESSAGE_FLAGS_DEFINITION, UNCERTAIN, UPDATED

def search_messages_searched(value: SearchMessagesOutput, failure: Failure | None = None) -> Case[SearchMessagesOutput]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_messages_searched, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def search_messages_provider_rejected(value: SearchMessagesOutput, failure: Failure | None = None) -> Case[SearchMessagesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_messages_provider_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def search_messages_invalid_response(value: SearchMessagesOutput, failure: Failure | None = None) -> Case[SearchMessagesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_messages_invalid_response, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def search_messages_defect(value: SearchMessagesOutput, failure: Failure | None = None) -> Case[SearchMessagesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_messages_defect, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def search_messages_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchMessagesOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_messages_retry, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def search_messages_customer_inbox() -> Case[SearchMessagesOutput]:
    return Case.paged(SEARCHED, (SearchMessagesOutput(folder='inbox', messages=(MessageSummary(id='AAMkAGI2MDEyMzQ1AAABC001', conversation_id='AAQkAGI2MDEyMzQ1CONV001', internet_message_id='<refund-request@mail.acme.example.com.au>', folder_id='AAMkAGI2MDEyMzQ1FOLDERINB', subject='Refund request', from_=EmailAddress(name='Jane Example', address='jane@acme.example.com.au'), sender=EmailAddress(name='Jane Example', address='jane@acme.example.com.au'), to=(EmailAddress(name='Contoso Support', address='support@contoso.example'),), received_at='2026-09-28T10:00:00Z', sent_at='2026-09-28T09:59:58Z', flag_status='notFlagged', importance='normal', preview='Please refund my last order.'),), has_more=True, next_page_cursor='https://graph.microsoft.com/v1.0/me/mailFolders/inbox/messages?$top=10&$skip=10'), SearchMessagesOutput(folder='inbox', messages=(MessageSummary(id='AAMkAGI2MDEyMzQ1AAABC002', conversation_id='AAQkAGI2MDEyMzQ1CONV002', internet_message_id='<refund-88213@mail.acme.example.com>', folder_id='AAMkAGI2MDEyMzQ1FOLDERINB', subject='Refund request for order 88213', from_=EmailAddress(name='Jane Smith', address='jane@acme.example.com'), sender=EmailAddress(name='Jane Smith', address='jane@acme.example.com'), to=(EmailAddress(name='Contoso Support', address='support@contoso.example'),), received_at='2026-09-28T09:00:00Z', sent_at='2026-09-28T08:59:57Z', flag_status='notFlagged', importance='normal', has_attachments=True, preview='I was charged twice for order 88213.'),), has_more=True, next_page_cursor='https://graph.microsoft.com/v1.0/me/mailFolders/inbox/messages?$top=10&$skip=20'), SearchMessagesOutput(folder='inbox', messages=(MessageSummary(id='AAMkAGI2MDEyMzQ1AAABC002', conversation_id='AAQkAGI2MDEyMzQ1CONV002', internet_message_id='<refund-88213@mail.acme.example.com>', folder_id='AAMkAGI2MDEyMzQ1FOLDERINB', subject='Refund request for order 88213', from_=EmailAddress(name='Jane Smith', address='jane@acme.example.com'), sender=EmailAddress(name='Jane Smith', address='jane@acme.example.com'), to=(EmailAddress(name='Contoso Support', address='support@contoso.example'),), received_at='2026-09-28T09:00:00Z', sent_at='2026-09-28T08:59:57Z', flag_status='notFlagged', importance='normal', has_attachments=True, preview='I was charged twice for order 88213.'), MessageSummary(id='AAMkAGI2MDEyMzQ1AAABC003', conversation_id='AAQkAGI2MDEyMzQ1CONV003', internet_message_id='<refund-question@mail.acme.example.com>', folder_id='AAMkAGI2MDEyMzQ1FOLDERINB', subject='Earlier refund question', from_=EmailAddress(name='Jane Smith', address='jane@acme.example.com'), sender=EmailAddress(name='Jane Smith', address='jane@acme.example.com'), to=(EmailAddress(name='Contoso Support', address='support@contoso.example'),), received_at='2026-09-26T09:00:00Z', sent_at='2026-09-26T08:59:58Z', is_read=True, flag_status='notFlagged', importance='normal', preview='Can I get a refund for a duplicate charge?')))))

setattr(search_messages_customer_inbox, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def search_messages_search_rejected() -> Case[SearchMessagesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SearchMessagesOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "searchMessages", "Microsoft Graph rejected the mailbox search."))

setattr(search_messages_search_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def get_message_found(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_message_found, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def get_message_not_found(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_message_not_found, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def get_message_provider_rejected(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_message_provider_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def get_message_invalid_response(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_message_invalid_response, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def get_message_defect(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_message_defect, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def get_message_retry(failure: Failure, after_seconds: float = 0) -> Case[Message]:
    return Case.retry(failure, after_seconds)

setattr(get_message_retry, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def get_message_customer_message() -> Case[Message]:
    return Case.branch_result(FOUND, Message(id='AAMkAGI2MDEyMzQ1AAABC002', conversation_id='AAQkAGI2MDEyMzQ1CONV002', internet_message_id='<refund-88213@mail.acme.example.com>', folder_id='AAMkAGI2MDEyMzQ1FOLDERINB', subject='Refund request for order 88213', from_=EmailAddress(name='Jane Smith', address='jane@acme.example.com'), sender=EmailAddress(name='Jane Smith', address='jane@acme.example.com'), to=(EmailAddress(name='Contoso Support', address='support@contoso.example'),), received_at='2026-09-28T09:00:00Z', sent_at='2026-09-28T08:59:57Z', flag_status='notFlagged', importance='normal', has_attachments=True, preview='I was charged twice for order 88213.', text='I was charged twice for order 88213.', text_source='text', web_link='https://outlook.office365.com/owa/?ItemID=AAMkAGI2MDEyMzQ1AAABC002&exvsurl=1&viewmodel=ReadMessageItem', attachments=(Attachment(id='AAMkAGI2MDEyMzQ1ATT0001', name='receipt.pdf', content_type='application/pdf', size_bytes=20480, kind='file'),)))

setattr(get_message_customer_message, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def get_message_message_missing() -> Case[Message]:
    return Case.branch_result(NOT_FOUND, Message(flag_status=''), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getMessage", "Microsoft Graph could not find the message."))

setattr(get_message_message_missing, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def get_message_message_read_rejected() -> Case[Message]:
    return Case.branch_result(PROVIDER_REJECTED, Message(flag_status=''), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getMessage", "Microsoft Graph rejected the message read."))

setattr(get_message_message_read_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def send_message_sent(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(SENT, value, failure)

setattr(send_message_sent, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def send_message_provider_rejected(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(send_message_provider_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def send_message_uncertain(value: SentMessage, failure: Failure) -> Case[SentMessage]:
    return Case.uncertain(value, failure)

setattr(send_message_uncertain, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def send_message_defect(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_message_defect, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def send_message_retry(failure: Failure, after_seconds: float = 0) -> Case[SentMessage]:
    return Case.retry(failure, after_seconds)

setattr(send_message_retry, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def send_message_new_message_sent() -> Case[SentMessage]:
    return Case.branch_result(SENT, SentMessage(message_id='AAMkAGI2MDEyMzQ1AAABC010', internet_message_id='<mock-new-88213@contoso.example>', conversation_id='AAQkAGI2MDEyMzQ1CONV010', subject='Your refund for order 88213', recipients=('jane@acme.example.com',), idempotency_marker='dex-8f14e45f-ceea-5a7d-9b1c-2f6e3d4a5b60'))

setattr(send_message_new_message_sent, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def send_message_send_rejected() -> Case[SentMessage]:
    return Case.branch_result(PROVIDER_REJECTED, SentMessage(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "sendMessage", "Microsoft Graph rejected the recipient."))

setattr(send_message_send_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def reply_to_message_sent(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(SENT, value, failure)

setattr(reply_to_message_sent, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def reply_to_message_not_found(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(reply_to_message_not_found, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def reply_to_message_provider_rejected(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(reply_to_message_provider_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def reply_to_message_uncertain(value: SentMessage, failure: Failure) -> Case[SentMessage]:
    return Case.uncertain(value, failure)

setattr(reply_to_message_uncertain, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def reply_to_message_defect(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(reply_to_message_defect, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def reply_to_message_retry(failure: Failure, after_seconds: float = 0) -> Case[SentMessage]:
    return Case.retry(failure, after_seconds)

setattr(reply_to_message_retry, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def reply_to_message_customer_reply_sent() -> Case[SentMessage]:
    return Case.branch_result(SENT, SentMessage(message_id='AAMkAGI2MDEyMzQ1AAABC011', internet_message_id='<mock-reply-88213@contoso.example>', conversation_id='AAQkAGI2MDEyMzQ1CONV002', subject='RE: Refund request for order 88213', recipients=('jane@acme.example.com',), idempotency_marker='dex-3c59dc04-8b2f-5e1a-a6d4-71b0c9e2f845'))

setattr(reply_to_message_customer_reply_sent, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def reply_to_message_original_message_missing() -> Case[SentMessage]:
    return Case.branch_result(NOT_FOUND, SentMessage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "replyToMessage", "Microsoft Graph could not find the original message."))

setattr(reply_to_message_original_message_missing, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def reply_to_message_reply_rejected() -> Case[SentMessage]:
    return Case.branch_result(PROVIDER_REJECTED, SentMessage(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "replyToMessage", "Microsoft Graph rejected the reply recipient."))

setattr(reply_to_message_reply_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def move_message_moved(value: MovedMessage, failure: Failure | None = None) -> Case[MovedMessage]:
    return Case.branch_result(MOVED, value, failure)

setattr(move_message_moved, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def move_message_not_found(value: MovedMessage, failure: Failure | None = None) -> Case[MovedMessage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(move_message_not_found, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def move_message_provider_rejected(value: MovedMessage, failure: Failure | None = None) -> Case[MovedMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(move_message_provider_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def move_message_defect(value: MovedMessage, failure: Failure | None = None) -> Case[MovedMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(move_message_defect, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def move_message_retry(failure: Failure, after_seconds: float = 0) -> Case[MovedMessage]:
    return Case.retry(failure, after_seconds)

setattr(move_message_retry, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def move_message_customer_message_archived() -> Case[MovedMessage]:
    return Case.branch_result(MOVED, MovedMessage(message_id='AAMkAGI2MDEyMzQ1AAABC002', destination_folder_id='AAMkAGI2MDEyMzQ1FOLDERARC', destination_folder_name='Archive'))

setattr(move_message_customer_message_archived, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def move_message_missing() -> Case[MovedMessage]:
    return Case.branch_result(NOT_FOUND, MovedMessage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "moveMessage", "Microsoft Graph could not find the message to move."))

setattr(move_message_missing, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def move_message_move_rejected() -> Case[MovedMessage]:
    return Case.branch_result(PROVIDER_REJECTED, MovedMessage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "moveMessage", "Microsoft Graph rejected the message move."))

setattr(move_message_move_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def set_message_flags_updated(value: MessageFlags, failure: Failure | None = None) -> Case[MessageFlags]:
    return Case.branch_result(UPDATED, value, failure)

setattr(set_message_flags_updated, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def set_message_flags_not_found(value: MessageFlags, failure: Failure | None = None) -> Case[MessageFlags]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(set_message_flags_not_found, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def set_message_flags_provider_rejected(value: MessageFlags, failure: Failure | None = None) -> Case[MessageFlags]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(set_message_flags_provider_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def set_message_flags_defect(value: MessageFlags, failure: Failure | None = None) -> Case[MessageFlags]:
    return Case.branch_result(DEFECT, value, failure)

setattr(set_message_flags_defect, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def set_message_flags_retry(failure: Failure, after_seconds: float = 0) -> Case[MessageFlags]:
    return Case.retry(failure, after_seconds)

setattr(set_message_flags_retry, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def set_message_flags_customer_message_read() -> Case[MessageFlags]:
    return Case.branch_result(UPDATED, MessageFlags(message_id='AAMkAGI2MDEyMzQ1AAABC002', is_read=True, flag_status='notFlagged'))

setattr(set_message_flags_customer_message_read, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def set_message_flags_flagged_message_missing() -> Case[MessageFlags]:
    return Case.branch_result(NOT_FOUND, MessageFlags(flag_status=''), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "setMessageFlags", "Microsoft Graph could not find the message to update."))

setattr(set_message_flags_flagged_message_missing, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

def set_message_flags_flags_rejected() -> Case[MessageFlags]:
    return Case.branch_result(PROVIDER_REJECTED, MessageFlags(flag_status=''), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "setMessageFlags", "Microsoft Graph rejected the message flag update."))

setattr(set_message_flags_flags_rejected, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")


class Mock:

    def __init__(self, connection_name: str = "outlook-mail") -> None:
        self.search_messages_script: Query[SearchMessagesInput, SearchMessagesOutput] = Query(SEARCH_MESSAGES_DEFINITION)
        self.search_messages_script.default(search_messages_customer_inbox())
        self.get_message_script: Query[GetMessageInput, Message] = Query(GET_MESSAGE_DEFINITION)
        self.get_message_script.default(get_message_customer_message())
        self.send_message_script: Mutation[SendMessageInput, SentMessage] = Mutation(SEND_MESSAGE_DEFINITION)
        self.send_message_script.default(send_message_new_message_sent())
        self.reply_to_message_script: Mutation[ReplyToMessageInput, SentMessage] = Mutation(REPLY_TO_MESSAGE_DEFINITION)
        self.reply_to_message_script.default(reply_to_message_customer_reply_sent())
        self.move_message_script: Mutation[MoveMessageInput, MovedMessage] = Mutation(MOVE_MESSAGE_DEFINITION)
        self.move_message_script.default(move_message_customer_message_archived())
        self.set_message_flags_script: Mutation[SetMessageFlagsInput, MessageFlags] = Mutation(SET_MESSAGE_FLAGS_DEFINITION)
        self.set_message_flags_script.default(set_message_flags_customer_message_read())
        self._connection = new_connection_with_operations(
            search_messages=self.search_messages_script,
            get_message=self.get_message_script,
            send_message=self.send_message_script,
            reply_to_message=self.reply_to_message_script,
            move_message=self.move_message_script,
            set_message_flags=self.set_message_flags_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.microsoft_outlook_mail.outlookmailmock")

__all__ = [
    "Mock",
    "search_messages_searched",
    "search_messages_provider_rejected",
    "search_messages_invalid_response",
    "search_messages_defect",
    "search_messages_retry",
    "search_messages_customer_inbox",
    "search_messages_search_rejected",
    "get_message_found",
    "get_message_not_found",
    "get_message_provider_rejected",
    "get_message_invalid_response",
    "get_message_defect",
    "get_message_retry",
    "get_message_customer_message",
    "get_message_message_missing",
    "get_message_message_read_rejected",
    "send_message_sent",
    "send_message_provider_rejected",
    "send_message_uncertain",
    "send_message_defect",
    "send_message_retry",
    "send_message_new_message_sent",
    "send_message_send_rejected",
    "reply_to_message_sent",
    "reply_to_message_not_found",
    "reply_to_message_provider_rejected",
    "reply_to_message_uncertain",
    "reply_to_message_defect",
    "reply_to_message_retry",
    "reply_to_message_customer_reply_sent",
    "reply_to_message_original_message_missing",
    "reply_to_message_reply_rejected",
    "move_message_moved",
    "move_message_not_found",
    "move_message_provider_rejected",
    "move_message_defect",
    "move_message_retry",
    "move_message_customer_message_archived",
    "move_message_missing",
    "move_message_move_rejected",
    "set_message_flags_updated",
    "set_message_flags_not_found",
    "set_message_flags_provider_rejected",
    "set_message_flags_defect",
    "set_message_flags_retry",
    "set_message_flags_customer_message_read",
    "set_message_flags_flagged_message_missing",
    "set_message_flags_flags_rejected",
]
