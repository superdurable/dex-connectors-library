# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddCommentInput, AddCommentOutput, Card, CreateCardInput, CreateCardOutput, GetCardInput, Label, ListCardsInput, ListCardsOutput, MemberReference, UpdateCardInput, UpdateCardOutput
from .definitions import CONNECTOR_ID, ADDED, ADD_COMMENT_DEFINITION, CREATED, CREATE_CARD_DEFINITION, DEFECT, FOUND, GET_CARD_DEFINITION, INVALID_RESPONSE, LISTED, LIST_CARDS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, UNCERTAIN, UPDATED, UPDATE_CARD_DEFINITION

def list_cards_listed(value: ListCardsOutput, failure: Failure | None = None) -> Case[ListCardsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_cards_listed, '__module__', "dex_connectors.atlassian_trello.trellomock")

def list_cards_not_found(value: ListCardsOutput, failure: Failure | None = None) -> Case[ListCardsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_cards_not_found, '__module__', "dex_connectors.atlassian_trello.trellomock")

def list_cards_provider_rejected(value: ListCardsOutput, failure: Failure | None = None) -> Case[ListCardsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_cards_provider_rejected, '__module__', "dex_connectors.atlassian_trello.trellomock")

def list_cards_invalid_response(value: ListCardsOutput, failure: Failure | None = None) -> Case[ListCardsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_cards_invalid_response, '__module__', "dex_connectors.atlassian_trello.trellomock")

def list_cards_defect(value: ListCardsOutput, failure: Failure | None = None) -> Case[ListCardsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_cards_defect, '__module__', "dex_connectors.atlassian_trello.trellomock")

def list_cards_retry(failure: Failure, after_seconds: float = 0) -> Case[ListCardsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_cards_retry, '__module__', "dex_connectors.atlassian_trello.trellomock")

def list_cards_approved_board_pages() -> Case[ListCardsOutput]:
    return Case.paged(LISTED, (ListCardsOutput(cards=(Card(id='6512f0a1c2d3e4f5a6b71002', name='[REQ-10420] Different request', board_id='6512f0a1c2d3e4f5a6b70001', list_id='6512f0a1c2d3e4f5a6b70101', is_closed=False, position=1024), Card(id='6512f0a1c2d3e4f5a6b71001', name='Lobby paint', board_id='6512f0a1c2d3e4f5a6b70001', list_id='6512f0a1c2d3e4f5a6b70101', is_closed=False, position=2048)), scanned_card_count=2, next_before='6512f0a1c2d3e4f5a6b71001'), ListCardsOutput(cards=(Card(id='6512f0a1c2d3e4f5a6b71000', name='Lobby paint', board_id='6512f0a1c2d3e4f5a6b70001', list_id='6512f0a1c2d3e4f5a6b70101', is_closed=False, position=2048), Card(id='6512f0a1c2d3e4f5a6b70fff', name='Door sensor', board_id='6512f0a1c2d3e4f5a6b70001', list_id='6512f0a1c2d3e4f5a6b70101', is_closed=False, position=3072)), scanned_card_count=2, next_before='6512f0a1c2d3e4f5a6b70fff'), ListCardsOutput(cards=(Card(id='6512f0a1c2d3e4f5a6b70ffe', name='[REQ-1042] Archived request', board_id='6512f0a1c2d3e4f5a6b70001', list_id='6512f0a1c2d3e4f5a6b70101', is_closed=True, position=4096),), scanned_card_count=1)))

setattr(list_cards_approved_board_pages, '__module__', "dex_connectors.atlassian_trello.trellomock")

def list_cards_not_found_failure() -> Case[ListCardsOutput]:
    return Case.branch_result(NOT_FOUND, ListCardsOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listCards", "The board or list does not exist."))

setattr(list_cards_not_found_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")

def list_cards_provider_rejected_failure() -> Case[ListCardsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListCardsOutput(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "listCards", "Trello conclusively rejected the list, such as an invalid key or token, or a board the token's member cannot open."))

setattr(list_cards_provider_rejected_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")

def list_cards_invalid_response_failure() -> Case[ListCardsOutput]:
    return Case.branch_result(INVALID_RESPONSE, ListCardsOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listCards", "Trello returned an invalid or oversized card page."))

setattr(list_cards_invalid_response_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")

def get_card_found(value: Card, failure: Failure | None = None) -> Case[Card]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_card_found, '__module__', "dex_connectors.atlassian_trello.trellomock")

def get_card_not_found(value: Card, failure: Failure | None = None) -> Case[Card]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_card_not_found, '__module__', "dex_connectors.atlassian_trello.trellomock")

def get_card_provider_rejected(value: Card, failure: Failure | None = None) -> Case[Card]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_card_provider_rejected, '__module__', "dex_connectors.atlassian_trello.trellomock")

def get_card_invalid_response(value: Card, failure: Failure | None = None) -> Case[Card]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_card_invalid_response, '__module__', "dex_connectors.atlassian_trello.trellomock")

def get_card_defect(value: Card, failure: Failure | None = None) -> Case[Card]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_card_defect, '__module__', "dex_connectors.atlassian_trello.trellomock")

def get_card_retry(failure: Failure, after_seconds: float = 0) -> Case[Card]:
    return Case.retry(failure, after_seconds)

setattr(get_card_retry, '__module__', "dex_connectors.atlassian_trello.trellomock")

def get_card_approved_request_card() -> Case[Card]:
    return Case.branch_result(FOUND, Card(id='6512f0a1c2d3e4f5a6b72001', name='[REQ-1042] Replace badge reader', board_id='6512f0a1c2d3e4f5a6b70001', list_id='6512f0a1c2d3e4f5a6b70102', is_closed=False, position=1024))

setattr(get_card_approved_request_card, '__module__', "dex_connectors.atlassian_trello.trellomock")

def get_card_not_found_failure() -> Case[Card]:
    return Case.branch_result(NOT_FOUND, Card(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getCard", "The card does not exist."))

setattr(get_card_not_found_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")

def get_card_provider_rejected_failure() -> Case[Card]:
    return Case.branch_result(PROVIDER_REJECTED, Card(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "getCard", "Trello conclusively rejected the card read, such as an invalid key or token, or a card the token's member cannot open."))

setattr(get_card_provider_rejected_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")

def get_card_invalid_response_failure() -> Case[Card]:
    return Case.branch_result(INVALID_RESPONSE, Card(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getCard", "Trello returned an invalid or oversized card."))

setattr(get_card_invalid_response_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")

def create_card_created(value: CreateCardOutput, failure: Failure | None = None) -> Case[CreateCardOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_card_created, '__module__', "dex_connectors.atlassian_trello.trellomock")

def create_card_provider_rejected(value: CreateCardOutput, failure: Failure | None = None) -> Case[CreateCardOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_card_provider_rejected, '__module__', "dex_connectors.atlassian_trello.trellomock")

def create_card_uncertain(value: CreateCardOutput, failure: Failure) -> Case[CreateCardOutput]:
    return Case.uncertain(value, failure)

setattr(create_card_uncertain, '__module__', "dex_connectors.atlassian_trello.trellomock")

def create_card_defect(value: CreateCardOutput, failure: Failure | None = None) -> Case[CreateCardOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_card_defect, '__module__', "dex_connectors.atlassian_trello.trellomock")

def create_card_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateCardOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_card_retry, '__module__', "dex_connectors.atlassian_trello.trellomock")

def create_card_approved_request_created() -> Case[CreateCardOutput]:
    return Case.branch_result(CREATED, CreateCardOutput(card_id='6512f0a1c2d3e4f5a6b72001', name='[REQ-1042] Replace badge reader', list_id='6512f0a1c2d3e4f5a6b70102', board_id='6512f0a1c2d3e4f5a6b70001', short_link='AbCd1234', url='https://trello.com/c/AbCd1234/approved-request', short_url='https://trello.com/c/AbCd1234'))

setattr(create_card_approved_request_created, '__module__', "dex_connectors.atlassian_trello.trellomock")

def create_card_provider_rejected_failure() -> Case[CreateCardOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateCardOutput(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "createCard", "Trello conclusively rejected the card, such as an unknown list, label, or member, or missing permission; nothing was created."))

setattr(create_card_provider_rejected_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")

def update_card_updated(value: UpdateCardOutput, failure: Failure | None = None) -> Case[UpdateCardOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_card_updated, '__module__', "dex_connectors.atlassian_trello.trellomock")

def update_card_not_found(value: UpdateCardOutput, failure: Failure | None = None) -> Case[UpdateCardOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_card_not_found, '__module__', "dex_connectors.atlassian_trello.trellomock")

def update_card_provider_rejected(value: UpdateCardOutput, failure: Failure | None = None) -> Case[UpdateCardOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_card_provider_rejected, '__module__', "dex_connectors.atlassian_trello.trellomock")

def update_card_invalid_response(value: UpdateCardOutput, failure: Failure | None = None) -> Case[UpdateCardOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_card_invalid_response, '__module__', "dex_connectors.atlassian_trello.trellomock")

def update_card_defect(value: UpdateCardOutput, failure: Failure | None = None) -> Case[UpdateCardOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_card_defect, '__module__', "dex_connectors.atlassian_trello.trellomock")

def update_card_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateCardOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_card_retry, '__module__', "dex_connectors.atlassian_trello.trellomock")

def update_card_approved_request_updated() -> Case[UpdateCardOutput]:
    return Case.branch_result(UPDATED, UpdateCardOutput(card_id='6512f0a1c2d3e4f5a6b72001', card=Card(id='6512f0a1c2d3e4f5a6b72001', name='[REQ-1042] Replace badge reader', board_id='6512f0a1c2d3e4f5a6b70001', list_id='6512f0a1c2d3e4f5a6b70102', is_closed=False, position=1024)))

setattr(update_card_approved_request_updated, '__module__', "dex_connectors.atlassian_trello.trellomock")

def update_card_not_found_failure() -> Case[UpdateCardOutput]:
    return Case.branch_result(NOT_FOUND, UpdateCardOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateCard", "The card does not exist."))

setattr(update_card_not_found_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")

def update_card_provider_rejected_failure() -> Case[UpdateCardOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateCardOutput(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "updateCard", "Trello conclusively rejected a change, such as an unknown list, label, or member, or missing permission."))

setattr(update_card_provider_rejected_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")

def update_card_invalid_response_failure() -> Case[UpdateCardOutput]:
    return Case.branch_result(INVALID_RESPONSE, UpdateCardOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "updateCard", "Trello returned an invalid or oversized card, or a card that does not show a requested change."))

setattr(update_card_invalid_response_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")

def add_comment_added(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_comment_added, '__module__', "dex_connectors.atlassian_trello.trellomock")

def add_comment_not_found(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_comment_not_found, '__module__', "dex_connectors.atlassian_trello.trellomock")

def add_comment_provider_rejected(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_comment_provider_rejected, '__module__', "dex_connectors.atlassian_trello.trellomock")

def add_comment_uncertain(value: AddCommentOutput, failure: Failure) -> Case[AddCommentOutput]:
    return Case.uncertain(value, failure)

setattr(add_comment_uncertain, '__module__', "dex_connectors.atlassian_trello.trellomock")

def add_comment_defect(value: AddCommentOutput, failure: Failure | None = None) -> Case[AddCommentOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_comment_defect, '__module__', "dex_connectors.atlassian_trello.trellomock")

def add_comment_retry(failure: Failure, after_seconds: float = 0) -> Case[AddCommentOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_comment_retry, '__module__', "dex_connectors.atlassian_trello.trellomock")

def add_comment_approval_comment_added() -> Case[AddCommentOutput]:
    return Case.branch_result(ADDED, AddCommentOutput(card_id='6512f0a1c2d3e4f5a6b72001', comment_id='6512f0a1c2d3e4f5a6b73001', author_member_id='6512f0a1c2d3e4f5a6b70301', created_at=datetime.fromisoformat("2026-10-26T16:20:00Z".replace('Z', '+00:00'))))

setattr(add_comment_approval_comment_added, '__module__', "dex_connectors.atlassian_trello.trellomock")

def add_comment_not_found_failure() -> Case[AddCommentOutput]:
    return Case.branch_result(NOT_FOUND, AddCommentOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addComment", "The card does not exist; nothing was added."))

setattr(add_comment_not_found_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")

def add_comment_provider_rejected_failure() -> Case[AddCommentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddCommentOutput(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "addComment", "Trello conclusively rejected the comment, such as missing permission or invalid text; nothing was added."))

setattr(add_comment_provider_rejected_failure, '__module__', "dex_connectors.atlassian_trello.trellomock")


class Mock:

    def __init__(self, connection_name: str = "trello") -> None:
        self.list_cards_script: Query[ListCardsInput, ListCardsOutput] = Query(LIST_CARDS_DEFINITION)
        self.list_cards_script.default(list_cards_approved_board_pages())
        self.get_card_script: Query[GetCardInput, Card] = Query(GET_CARD_DEFINITION)
        self.get_card_script.default(get_card_approved_request_card())
        self.create_card_script: Mutation[CreateCardInput, CreateCardOutput] = Mutation(CREATE_CARD_DEFINITION)
        self.create_card_script.default(create_card_approved_request_created())
        self.update_card_script: Mutation[UpdateCardInput, UpdateCardOutput] = Mutation(UPDATE_CARD_DEFINITION)
        self.update_card_script.default(update_card_approved_request_updated())
        self.add_comment_script: Mutation[AddCommentInput, AddCommentOutput] = Mutation(ADD_COMMENT_DEFINITION)
        self.add_comment_script.default(add_comment_approval_comment_added())
        self._connection = new_connection_with_operations(
            list_cards=self.list_cards_script,
            get_card=self.get_card_script,
            create_card=self.create_card_script,
            update_card=self.update_card_script,
            add_comment=self.add_comment_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.atlassian_trello.trellomock")

__all__ = [
    "Mock",
    "list_cards_listed",
    "list_cards_not_found",
    "list_cards_provider_rejected",
    "list_cards_invalid_response",
    "list_cards_defect",
    "list_cards_retry",
    "list_cards_approved_board_pages",
    "list_cards_not_found_failure",
    "list_cards_provider_rejected_failure",
    "list_cards_invalid_response_failure",
    "get_card_found",
    "get_card_not_found",
    "get_card_provider_rejected",
    "get_card_invalid_response",
    "get_card_defect",
    "get_card_retry",
    "get_card_approved_request_card",
    "get_card_not_found_failure",
    "get_card_provider_rejected_failure",
    "get_card_invalid_response_failure",
    "create_card_created",
    "create_card_provider_rejected",
    "create_card_uncertain",
    "create_card_defect",
    "create_card_retry",
    "create_card_approved_request_created",
    "create_card_provider_rejected_failure",
    "update_card_updated",
    "update_card_not_found",
    "update_card_provider_rejected",
    "update_card_invalid_response",
    "update_card_defect",
    "update_card_retry",
    "update_card_approved_request_updated",
    "update_card_not_found_failure",
    "update_card_provider_rejected_failure",
    "update_card_invalid_response_failure",
    "add_comment_added",
    "add_comment_not_found",
    "add_comment_provider_rejected",
    "add_comment_uncertain",
    "add_comment_defect",
    "add_comment_retry",
    "add_comment_approval_comment_added",
    "add_comment_not_found_failure",
    "add_comment_provider_rejected_failure",
]
