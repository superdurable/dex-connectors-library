# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CaptureEventInput, CapturedEvent, InsightMetadata, InsightsPage, ListInsightsInput
from .definitions import CONNECTOR_ID, ACCEPTED, CAPTURE_EVENT_DEFINITION, DEFECT, INVALID_RESPONSE, LISTED, LIST_INSIGHTS_DEFINITION, PROVIDER_REJECTED, QUOTA_LIMITED

def capture_event_accepted(value: CapturedEvent, failure: Failure | None = None) -> Case[CapturedEvent]:
    return Case.branch_result(ACCEPTED, value, failure)

setattr(capture_event_accepted, '__module__', "dex_connectors.posthog.posthogmock")

def capture_event_quota_limited(value: CapturedEvent, failure: Failure | None = None) -> Case[CapturedEvent]:
    return Case.branch_result(QUOTA_LIMITED, value, failure)

setattr(capture_event_quota_limited, '__module__', "dex_connectors.posthog.posthogmock")

def capture_event_provider_rejected(value: CapturedEvent, failure: Failure | None = None) -> Case[CapturedEvent]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(capture_event_provider_rejected, '__module__', "dex_connectors.posthog.posthogmock")

def capture_event_defect(value: CapturedEvent, failure: Failure | None = None) -> Case[CapturedEvent]:
    return Case.branch_result(DEFECT, value, failure)

setattr(capture_event_defect, '__module__', "dex_connectors.posthog.posthogmock")

def capture_event_retry(failure: Failure, after_seconds: float = 0) -> Case[CapturedEvent]:
    return Case.retry(failure, after_seconds)

setattr(capture_event_retry, '__module__', "dex_connectors.posthog.posthogmock")

def capture_event_accepted_signup() -> Case[CapturedEvent]:
    return Case.branch_result(ACCEPTED, CapturedEvent(event_id='4b5d3f33-2e21-4c1c-b392-2dae2a74a2ed', accepted=True))

setattr(capture_event_accepted_signup, '__module__', "dex_connectors.posthog.posthogmock")

def list_insights_listed(value: InsightsPage, failure: Failure | None = None) -> Case[InsightsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_insights_listed, '__module__', "dex_connectors.posthog.posthogmock")

def list_insights_provider_rejected(value: InsightsPage, failure: Failure | None = None) -> Case[InsightsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_insights_provider_rejected, '__module__', "dex_connectors.posthog.posthogmock")

def list_insights_invalid_response(value: InsightsPage, failure: Failure | None = None) -> Case[InsightsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_insights_invalid_response, '__module__', "dex_connectors.posthog.posthogmock")

def list_insights_defect(value: InsightsPage, failure: Failure | None = None) -> Case[InsightsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_insights_defect, '__module__', "dex_connectors.posthog.posthogmock")

def list_insights_retry(failure: Failure, after_seconds: float = 0) -> Case[InsightsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_insights_retry, '__module__', "dex_connectors.posthog.posthogmock")

def list_insights_listed_signup_insight() -> Case[InsightsPage]:
    return Case.branch_result(LISTED, InsightsPage(insights=(InsightMetadata(id=42, name='Signups by day'),), has_more=False, offset=0, limit=20))

setattr(list_insights_listed_signup_insight, '__module__', "dex_connectors.posthog.posthogmock")


class Mock:

    def __init__(self, connection_name: str = "posthog") -> None:
        self.capture_event_script: Mutation[CaptureEventInput, CapturedEvent] = Mutation(CAPTURE_EVENT_DEFINITION)
        self.capture_event_script.default(capture_event_accepted_signup())
        self.list_insights_script: Query[ListInsightsInput, InsightsPage] = Query(LIST_INSIGHTS_DEFINITION)
        self.list_insights_script.default(list_insights_listed_signup_insight())
        self._connection = new_connection_with_operations(
            capture_event=self.capture_event_script,
            list_insights=self.list_insights_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.posthog.posthogmock")

__all__ = [
    "Mock",
    "capture_event_accepted",
    "capture_event_quota_limited",
    "capture_event_provider_rejected",
    "capture_event_defect",
    "capture_event_retry",
    "capture_event_accepted_signup",
    "list_insights_listed",
    "list_insights_provider_rejected",
    "list_insights_invalid_response",
    "list_insights_defect",
    "list_insights_retry",
    "list_insights_listed_signup_insight",
]
