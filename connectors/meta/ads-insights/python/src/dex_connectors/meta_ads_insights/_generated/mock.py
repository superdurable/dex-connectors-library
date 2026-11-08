# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AdMetadata, GetAdInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_AD_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_ad_found(value: AdMetadata, failure: Failure | None = None) -> Case[AdMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_ad_found, '__module__', "dex_connectors.meta_ads_insights.metaadsinsightsmock")

def get_ad_not_found(value: AdMetadata, failure: Failure | None = None) -> Case[AdMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_ad_not_found, '__module__', "dex_connectors.meta_ads_insights.metaadsinsightsmock")

def get_ad_provider_rejected(value: AdMetadata, failure: Failure | None = None) -> Case[AdMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_ad_provider_rejected, '__module__', "dex_connectors.meta_ads_insights.metaadsinsightsmock")

def get_ad_invalid_response(value: AdMetadata, failure: Failure | None = None) -> Case[AdMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_ad_invalid_response, '__module__', "dex_connectors.meta_ads_insights.metaadsinsightsmock")

def get_ad_defect(value: AdMetadata, failure: Failure | None = None) -> Case[AdMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_ad_defect, '__module__', "dex_connectors.meta_ads_insights.metaadsinsightsmock")

def get_ad_retry(failure: Failure, after_seconds: float = 0) -> Case[AdMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_ad_retry, '__module__', "dex_connectors.meta_ads_insights.metaadsinsightsmock")

def get_ad_found_default() -> Case[AdMetadata]:
    return Case.branch_result(FOUND, AdMetadata(id='123456789012345', name='Spring campaign image', status='ACTIVE', effective_status='ACTIVE'))

setattr(get_ad_found_default, '__module__', "dex_connectors.meta_ads_insights.metaadsinsightsmock")


class Mock:

    def __init__(self, connection_name: str = "meta-ads-insights") -> None:
        self.get_ad_script: Query[GetAdInput, AdMetadata] = Query(GET_AD_DEFINITION)
        self.get_ad_script.default(get_ad_found_default())
        self._connection = new_connection_with_operations(
            get_ad=self.get_ad_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.meta_ads_insights.metaadsinsightsmock")

__all__ = [
    "Mock",
    "get_ad_found",
    "get_ad_not_found",
    "get_ad_provider_rejected",
    "get_ad_invalid_response",
    "get_ad_defect",
    "get_ad_retry",
    "get_ad_found_default",
]
