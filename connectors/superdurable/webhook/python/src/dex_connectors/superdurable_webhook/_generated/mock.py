# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation

from ..connection import Connection, new_connection_with_operations
from ..models import SendEventInput, SendEventOutput
from .definitions import CONNECTOR_ID, DEFECT, DELIVERED, REJECTED, SEND_EVENT_DEFINITION

def send_event_delivered(value: SendEventOutput, failure: Failure | None = None) -> Case[SendEventOutput]:
    return Case.branch_result(DELIVERED, value, failure)

setattr(send_event_delivered, '__module__', "dex_connectors.superdurable_webhook.webhookmock")

def send_event_rejected(value: SendEventOutput, failure: Failure | None = None) -> Case[SendEventOutput]:
    return Case.branch_result(REJECTED, value, failure)

setattr(send_event_rejected, '__module__', "dex_connectors.superdurable_webhook.webhookmock")

def send_event_defect(value: SendEventOutput, failure: Failure | None = None) -> Case[SendEventOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_event_defect, '__module__', "dex_connectors.superdurable_webhook.webhookmock")

def send_event_retry(failure: Failure, after_seconds: float = 0) -> Case[SendEventOutput]:
    return Case.retry(failure, after_seconds)

setattr(send_event_retry, '__module__', "dex_connectors.superdurable_webhook.webhookmock")

def send_event_submission_delivered() -> Case[SendEventOutput]:
    return Case.branch_result(DELIVERED, SendEventOutput(webhook_id='msg_00000000000000000000000000000001', status_code=202))

setattr(send_event_submission_delivered, '__module__', "dex_connectors.superdurable_webhook.webhookmock")

def send_event_submission_rejected() -> Case[SendEventOutput]:
    return Case.branch_result(REJECTED, SendEventOutput(webhook_id='msg_00000000000000000000000000000002', status_code=422), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "sendEvent", "The receiver answered HTTP 422."))

setattr(send_event_submission_rejected, '__module__', "dex_connectors.superdurable_webhook.webhookmock")


class Mock:

    def __init__(self, connection_name: str = "webhook") -> None:
        self.send_event_script: Mutation[SendEventInput, SendEventOutput] = Mutation(SEND_EVENT_DEFINITION)
        self.send_event_script.default(send_event_submission_delivered())
        self._connection = new_connection_with_operations(
            send_event=self.send_event_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.superdurable_webhook.webhookmock")

__all__ = [
    "Mock",
    "send_event_delivered",
    "send_event_rejected",
    "send_event_defect",
    "send_event_retry",
    "send_event_submission_delivered",
    "send_event_submission_rejected",
]
