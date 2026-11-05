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

CONNECTOR_ID = "docusign"
PROVIDER = "docusign"
CREATE_ENVELOPE_FROM_TEMPLATE_OPERATION_ID = "createEnvelopeFromTemplate"
GET_ENVELOPE_OPERATION_ID = "getEnvelope"
LIST_ENVELOPE_RECIPIENTS_OPERATION_ID = "listEnvelopeRecipients"
VOID_ENVELOPE_OPERATION_ID = "voidEnvelope"
DOWNLOAD_COMBINED_DOCUMENT_OPERATION_ID = "downloadCombinedDocument"
CREATED = "created"
DEFECT = "defect"
DOWNLOADED = "downloaded"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
NOT_VOIDABLE = "notVoidable"
PROVIDER_REJECTED = "providerRejected"
TOO_LARGE = "tooLarge"
UNCERTAIN = "uncertain"
VOIDED = "voided"

CREATE_ENVELOPE_FROM_TEMPLATE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_ENVELOPE_FROM_TEMPLATE_OPERATION_ID),
    (
        BranchDefinition(CREATED, "DocuSign holds the envelope this Step execution created; wasRecovered tells whether a later attempt found it by its custom field after an interrupted one.", False),
        BranchDefinition(PROVIDER_REJECTED, "DocuSign conclusively rejected the envelope, such as an unknown template, an unfilled or unknown role, an invalid email address, an exhausted envelope allowance, or a user who may not send; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "The create request was sent but DocuSign's answer was lost, a server error, or unreadable, and no envelope with this Step's custom field could be found, so an envelope may exist that this Result does not name; do not create another automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=60),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 6, timedelta(seconds=3900)),
    ),
)

GET_ENVELOPE_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_ENVELOPE_OPERATION_ID),
    (
        BranchDefinition(FOUND, "DocuSign returned the envelope.", False),
        BranchDefinition(NOT_FOUND, "The envelope does not exist in the connection's account or the user may not see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "DocuSign conclusively rejected the read, such as a revoked consent or a user without access to the account.", True),
        BranchDefinition(INVALID_RESPONSE, "DocuSign returned a malformed or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 0, timedelta(seconds=3900)),
    ),
)

LIST_ENVELOPE_RECIPIENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_ENVELOPE_RECIPIENTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "DocuSign returned the envelope's recipients.", False),
        BranchDefinition(NOT_FOUND, "The envelope does not exist in the connection's account or the user may not see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "DocuSign conclusively rejected the read, such as a revoked consent or a user without access to the account.", True),
        BranchDefinition(INVALID_RESPONSE, "DocuSign returned a malformed or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 0, timedelta(seconds=3900)),
    ),
)

VOID_ENVELOPE_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, VOID_ENVELOPE_OPERATION_ID),
    (
        BranchDefinition(VOIDED, "The envelope is voided, by this call or earlier; wasAlreadyVoided tells which.", False),
        BranchDefinition(NOT_VOIDABLE, "The envelope is completed, declined, or still a draft, which DocuSign does not void; status names which.", True),
        BranchDefinition(NOT_FOUND, "The envelope does not exist in the connection's account or the user may not see it.", True),
        BranchDefinition(PROVIDER_REJECTED, "DocuSign conclusively rejected the void, such as a user who is not the sender or lacks permission.", True),
        BranchDefinition(INVALID_RESPONSE, "DocuSign returned a malformed or oversized response.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 0, timedelta(seconds=3900)),
    ),
)

DOWNLOAD_COMBINED_DOCUMENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, DOWNLOAD_COMBINED_DOCUMENT_OPERATION_ID),
    (
        BranchDefinition(DOWNLOADED, "DocuSign returned the combined PDF and its digest was recorded, and stored when the client has a CombinedDocumentStore.", False),
        BranchDefinition(NOT_FOUND, "The envelope does not exist in the connection's account or the user may not see it.", True),
        BranchDefinition(TOO_LARGE, "The combined PDF exceeds the connection's maxDocumentBytes; nothing was stored.", True),
        BranchDefinition(PROVIDER_REJECTED, "DocuSign conclusively rejected the download, such as a revoked consent or a user without access to the envelope.", True),
        BranchDefinition(INVALID_RESPONSE, "DocuSign returned something other than a complete PDF, or the CombinedDocumentStore rejected it.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=120),
        2,
        heartbeat_timeout=timedelta(seconds=30),
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=60), 0, timedelta(seconds=3900)),
    ),
)
