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
    TriggerDefinition,
    TriggerRef,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "docusign"
PROVIDER = "docusign"
GET_ENVELOPE_OPERATION_ID = "getEnvelope"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"

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

ENVELOPE_EVENT_RECEIVED_TRIGGER_DEFINITION = TriggerDefinition(
    trigger=TriggerRef(connector_id=CONNECTOR_ID, trigger_name="envelopeEventReceived"),
    description="Receive an HMAC-verified DocuSign Connect envelope-completed, envelope-declined, or envelope-voided event in the JSON SIM format, with the envelope ID and event name as its event ID and the envelope's text custom fields for correlation.",
)
