# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CheckIPReputationInput, CheckIPReputationOutput
from .definitions import CONNECTOR_ID, CHECK_IP_REPUTATION_DEFINITION, DEFECT, FOUND, INVALID_RESPONSE, NO_DATA, PROVIDER_REJECTED

def check_ip_reputation_found(value: CheckIPReputationOutput, failure: Failure | None = None) -> Case[CheckIPReputationOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(check_ip_reputation_found, '__module__', "dex_connectors.threatbook.threatbookmock")

def check_ip_reputation_no_data(value: CheckIPReputationOutput, failure: Failure | None = None) -> Case[CheckIPReputationOutput]:
    return Case.branch_result(NO_DATA, value, failure)

setattr(check_ip_reputation_no_data, '__module__', "dex_connectors.threatbook.threatbookmock")

def check_ip_reputation_provider_rejected(value: CheckIPReputationOutput, failure: Failure | None = None) -> Case[CheckIPReputationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(check_ip_reputation_provider_rejected, '__module__', "dex_connectors.threatbook.threatbookmock")

def check_ip_reputation_invalid_response(value: CheckIPReputationOutput, failure: Failure | None = None) -> Case[CheckIPReputationOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(check_ip_reputation_invalid_response, '__module__', "dex_connectors.threatbook.threatbookmock")

def check_ip_reputation_defect(value: CheckIPReputationOutput, failure: Failure | None = None) -> Case[CheckIPReputationOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(check_ip_reputation_defect, '__module__', "dex_connectors.threatbook.threatbookmock")

def check_ip_reputation_retry(failure: Failure, after_seconds: float = 0) -> Case[CheckIPReputationOutput]:
    return Case.retry(failure, after_seconds)

setattr(check_ip_reputation_retry, '__module__', "dex_connectors.threatbook.threatbookmock")

def check_ip_reputation_suspicious_ip() -> Case[CheckIPReputationOutput]:
    return Case.branch_result(FOUND, CheckIPReputationOutput(is_malicious=True, confidence_level='high', severity='medium', judgments=('Scanner',)))

setattr(check_ip_reputation_suspicious_ip, '__module__', "dex_connectors.threatbook.threatbookmock")

def check_ip_reputation_no_intelligence() -> Case[CheckIPReputationOutput]:
    return Case.branch_result(NO_DATA, CheckIPReputationOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "checkIPReputation", "ThreatBook has no intelligence for this IP."))

setattr(check_ip_reputation_no_intelligence, '__module__', "dex_connectors.threatbook.threatbookmock")


class Mock:

    def __init__(self, connection_name: str = "threatbook") -> None:
        self.check_ip_reputation_script: Query[CheckIPReputationInput, CheckIPReputationOutput] = Query(CHECK_IP_REPUTATION_DEFINITION)
        self.check_ip_reputation_script.default(check_ip_reputation_suspicious_ip())
        self._connection = new_connection_with_operations(
            check_ip_reputation=self.check_ip_reputation_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.threatbook.threatbookmock")

__all__ = [
    "Mock",
    "check_ip_reputation_found",
    "check_ip_reputation_no_data",
    "check_ip_reputation_provider_rejected",
    "check_ip_reputation_invalid_response",
    "check_ip_reputation_defect",
    "check_ip_reputation_retry",
    "check_ip_reputation_suspicious_ip",
    "check_ip_reputation_no_intelligence",
]
