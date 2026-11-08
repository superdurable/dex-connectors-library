# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AgreementStatus, GetAgreementInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_AGREEMENT_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_agreement_found(value: AgreementStatus, failure: Failure | None = None) -> Case[AgreementStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_agreement_found, '__module__', "dex_connectors.adobe_acrobat_sign.adobe_acrobat_signmock")

def get_agreement_not_found(value: AgreementStatus, failure: Failure | None = None) -> Case[AgreementStatus]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_agreement_not_found, '__module__', "dex_connectors.adobe_acrobat_sign.adobe_acrobat_signmock")

def get_agreement_provider_rejected(value: AgreementStatus, failure: Failure | None = None) -> Case[AgreementStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_agreement_provider_rejected, '__module__', "dex_connectors.adobe_acrobat_sign.adobe_acrobat_signmock")

def get_agreement_invalid_response(value: AgreementStatus, failure: Failure | None = None) -> Case[AgreementStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_agreement_invalid_response, '__module__', "dex_connectors.adobe_acrobat_sign.adobe_acrobat_signmock")

def get_agreement_defect(value: AgreementStatus, failure: Failure | None = None) -> Case[AgreementStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_agreement_defect, '__module__', "dex_connectors.adobe_acrobat_sign.adobe_acrobat_signmock")

def get_agreement_retry(failure: Failure, after_seconds: float = 0) -> Case[AgreementStatus]:
    return Case.retry(failure, after_seconds)

setattr(get_agreement_retry, '__module__', "dex_connectors.adobe_acrobat_sign.adobe_acrobat_signmock")

def get_agreement_found_default() -> Case[AgreementStatus]:
    return Case.branch_result(FOUND, AgreementStatus(id='3AAABLblqZNOTREALAGREEMENTID5_BjiH', name='Example agreement', status='SIGNED'))

setattr(get_agreement_found_default, '__module__', "dex_connectors.adobe_acrobat_sign.adobe_acrobat_signmock")


class Mock:

    def __init__(self, connection_name: str = "adobe-acrobat-sign") -> None:
        self.get_agreement_script: Query[GetAgreementInput, AgreementStatus] = Query(GET_AGREEMENT_DEFINITION)
        self.get_agreement_script.default(get_agreement_found_default())
        self._connection = new_connection_with_operations(
            get_agreement=self.get_agreement_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.adobe_acrobat_sign.adobe_acrobat_signmock")

__all__ = [
    "Mock",
    "get_agreement_found",
    "get_agreement_not_found",
    "get_agreement_provider_rejected",
    "get_agreement_invalid_response",
    "get_agreement_defect",
    "get_agreement_retry",
    "get_agreement_found_default",
]
