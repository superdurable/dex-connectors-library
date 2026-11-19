# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Campaign, ListCampaignsInput, ListCampaignsOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_CAMPAIGNS_DEFINITION, PROVIDER_REJECTED

def list_campaigns_listed(value: ListCampaignsOutput, failure: Failure | None = None) -> Case[ListCampaignsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_campaigns_listed, '__module__', "dex_connectors.sensorsdata.sensorsdatamock")

def list_campaigns_provider_rejected(value: ListCampaignsOutput, failure: Failure | None = None) -> Case[ListCampaignsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_campaigns_provider_rejected, '__module__', "dex_connectors.sensorsdata.sensorsdatamock")

def list_campaigns_invalid_response(value: ListCampaignsOutput, failure: Failure | None = None) -> Case[ListCampaignsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_campaigns_invalid_response, '__module__', "dex_connectors.sensorsdata.sensorsdatamock")

def list_campaigns_defect(value: ListCampaignsOutput, failure: Failure | None = None) -> Case[ListCampaignsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_campaigns_defect, '__module__', "dex_connectors.sensorsdata.sensorsdatamock")

def list_campaigns_retry(failure: Failure, after_seconds: float = 0) -> Case[ListCampaignsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_campaigns_retry, '__module__', "dex_connectors.sensorsdata.sensorsdatamock")

def list_campaigns_sample_campaigns() -> Case[ListCampaignsOutput]:
    return Case.branch_result(LISTED, ListCampaignsOutput(campaigns=(Campaign(name='Example Campaign', link_count=1, latest_use_time='2025-01-01 12:00:00'),), total_rows=1, total_pages=1, page_num=0, page_size=20))

setattr(list_campaigns_sample_campaigns, '__module__', "dex_connectors.sensorsdata.sensorsdatamock")


class Mock:

    def __init__(self, connection_name: str = "sensorsdata") -> None:
        self.list_campaigns_script: Query[ListCampaignsInput, ListCampaignsOutput] = Query(LIST_CAMPAIGNS_DEFINITION)
        self.list_campaigns_script.default(list_campaigns_sample_campaigns())
        self._connection = new_connection_with_operations(
            list_campaigns=self.list_campaigns_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.sensorsdata.sensorsdatamock")

__all__ = [
    "Mock",
    "list_campaigns_listed",
    "list_campaigns_provider_rejected",
    "list_campaigns_invalid_response",
    "list_campaigns_defect",
    "list_campaigns_retry",
    "list_campaigns_sample_campaigns",
]
