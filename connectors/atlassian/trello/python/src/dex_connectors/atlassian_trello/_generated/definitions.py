# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import timedelta

from dex_connectors.sdk import (
    BranchDefinition,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
    RetryPolicy,
    StepDefaults,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "trello"
PROVIDER = "trello"
LIST_CARDS_OPERATION_ID = "listCards"
GET_CARD_OPERATION_ID = "getCard"
CREATE_CARD_OPERATION_ID = "createCard"
UPDATE_CARD_OPERATION_ID = "updateCard"
ADD_COMMENT_OPERATION_ID = "addComment"
ADDED = "added"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UNCERTAIN = "uncertain"
UPDATED = "updated"

LIST_CARDS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_CARDS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of the board's or list's cards was read and filtered.", False),
        BranchDefinition(NOT_FOUND, "The board or list does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Trello conclusively rejected the list, such as an invalid key or token, or a board the token's member cannot open.", True),
        BranchDefinition(INVALID_RESPONSE, "Trello returned an invalid or oversized card page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_CARD_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_CARD_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The card was read.", False),
        BranchDefinition(NOT_FOUND, "The card does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Trello conclusively rejected the card read, such as an invalid key or token, or a card the token's member cannot open.", True),
        BranchDefinition(INVALID_RESPONSE, "Trello returned an invalid or oversized card.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_CARD_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_CARD_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Trello created the card and returned its ID.", False),
        BranchDefinition(PROVIDER_REJECTED, "Trello conclusively rejected the card, such as an unknown list, label, or member, or missing permission; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "The request was sent but its outcome cannot be confirmed; Trello has no idempotency key, so the card may exist and must not be created again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

UPDATE_CARD_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_CARD_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Trello applied every requested change, including when the card already had those values, and the Value holds the card Trello returned.", False),
        BranchDefinition(NOT_FOUND, "The card does not exist.", True),
        BranchDefinition(PROVIDER_REJECTED, "Trello conclusively rejected a change, such as an unknown list, label, or member, or missing permission.", True),
        BranchDefinition(INVALID_RESPONSE, "Trello returned an invalid or oversized card, or a card that does not show a requested change.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

ADD_COMMENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, ADD_COMMENT_OPERATION_ID),
    (
        BranchDefinition(ADDED, "Trello added the comment and returned its action ID.", False),
        BranchDefinition(NOT_FOUND, "The card does not exist; nothing was added.", True),
        BranchDefinition(PROVIDER_REJECTED, "Trello conclusively rejected the comment, such as missing permission or invalid text; nothing was added.", True),
        BranchDefinition(UNCERTAIN, "The request was sent but its outcome cannot be confirmed; Trello has no idempotency key, so the comment may exist and must not be added again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)
