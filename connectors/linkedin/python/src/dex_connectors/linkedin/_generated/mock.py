# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AuthenticatedProfile, GetAuthenticatedProfileInput
from .definitions import CONNECTOR_ID, AUTHORIZATION_REVOKED, DEFECT, GET_AUTHENTICATED_PROFILE_DEFINITION, INSUFFICIENT_SCOPE, INVALID_RESPONSE, NOT_FOUND, PROFILE_LOADED, PROVIDER_REJECTED, VERIFIED_EMAIL_REQUIRED

def get_authenticated_profile_profile_loaded(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(PROFILE_LOADED, value, failure)

setattr(get_authenticated_profile_profile_loaded, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_verified_email_required(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(VERIFIED_EMAIL_REQUIRED, value, failure)

setattr(get_authenticated_profile_verified_email_required, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_insufficient_scope(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(INSUFFICIENT_SCOPE, value, failure)

setattr(get_authenticated_profile_insufficient_scope, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_authorization_revoked(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(AUTHORIZATION_REVOKED, value, failure)

setattr(get_authenticated_profile_authorization_revoked, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_not_found(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_authenticated_profile_not_found, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_provider_rejected(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_authenticated_profile_provider_rejected, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_invalid_response(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_authenticated_profile_invalid_response, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_defect(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_authenticated_profile_defect, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_retry(failure: Failure, after_seconds: float = 0) -> Case[AuthenticatedProfile]:
    return Case.retry(failure, after_seconds)

setattr(get_authenticated_profile_retry, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_verified_signup_profile() -> Case[AuthenticatedProfile]:
    return Case.branch_result(PROFILE_LOADED, AuthenticatedProfile(subject='linkedin-member-ada', name='Ada Lovelace', given_name='Ada', family_name='Lovelace', picture_url='https://media.licdn.com/dms/image/ada/profile-displayphoto-shrink_100_100/0/ada', locale='en_US', verified_email='ada@example.com'))

setattr(get_authenticated_profile_verified_signup_profile, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_member_without_verified_email() -> Case[AuthenticatedProfile]:
    return Case.branch_result(VERIFIED_EMAIL_REQUIRED, AuthenticatedProfile(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getAuthenticatedProfile", "LinkedIn verified email is required."))

setattr(get_authenticated_profile_member_without_verified_email, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_grant_without_email_scope() -> Case[AuthenticatedProfile]:
    return Case.branch_result(VERIFIED_EMAIL_REQUIRED, AuthenticatedProfile(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getAuthenticatedProfile", "LinkedIn verified email is required."))

setattr(get_authenticated_profile_grant_without_email_scope, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_forbidden_user_info_request() -> Case[AuthenticatedProfile]:
    return Case.branch_result(INSUFFICIENT_SCOPE, AuthenticatedProfile(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getAuthenticatedProfile", "LinkedIn authorization is forbidden or lacks scope."))

setattr(get_authenticated_profile_forbidden_user_info_request, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_revoked_member_grant() -> Case[AuthenticatedProfile]:
    return Case.branch_result(AUTHORIZATION_REVOKED, AuthenticatedProfile(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getAuthenticatedProfile", "LinkedIn authorization is invalid or revoked."))

setattr(get_authenticated_profile_revoked_member_grant, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_missing_member() -> Case[AuthenticatedProfile]:
    return Case.branch_result(NOT_FOUND, AuthenticatedProfile(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getAuthenticatedProfile", "LinkedIn member was not found."))

setattr(get_authenticated_profile_missing_member, '__module__', "dex_connectors.linkedin.linkedinmock")

def get_authenticated_profile_rejected_user_info_request() -> Case[AuthenticatedProfile]:
    return Case.branch_result(PROVIDER_REJECTED, AuthenticatedProfile(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "getAuthenticatedProfile", "LinkedIn rejected the UserInfo request."))

setattr(get_authenticated_profile_rejected_user_info_request, '__module__', "dex_connectors.linkedin.linkedinmock")


class Mock:

    def __init__(self, connection_name: str = "linkedin") -> None:
        self.get_authenticated_profile_script: Query[GetAuthenticatedProfileInput, AuthenticatedProfile] = Query(GET_AUTHENTICATED_PROFILE_DEFINITION)
        self.get_authenticated_profile_script.default(get_authenticated_profile_verified_signup_profile())
        self._connection = new_connection_with_operations(
            get_authenticated_profile=self.get_authenticated_profile_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.linkedin.linkedinmock")

__all__ = [
    "Mock",
    "get_authenticated_profile_profile_loaded",
    "get_authenticated_profile_verified_email_required",
    "get_authenticated_profile_insufficient_scope",
    "get_authenticated_profile_authorization_revoked",
    "get_authenticated_profile_not_found",
    "get_authenticated_profile_provider_rejected",
    "get_authenticated_profile_invalid_response",
    "get_authenticated_profile_defect",
    "get_authenticated_profile_retry",
    "get_authenticated_profile_verified_signup_profile",
    "get_authenticated_profile_member_without_verified_email",
    "get_authenticated_profile_grant_without_email_scope",
    "get_authenticated_profile_forbidden_user_info_request",
    "get_authenticated_profile_revoked_member_grant",
    "get_authenticated_profile_missing_member",
    "get_authenticated_profile_rejected_user_info_request",
]
