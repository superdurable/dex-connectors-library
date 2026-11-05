# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddUserToGroupInput, AddUserToGroupOutput, CreateUserInput, CreateUserOutput, DisableUserInput, EnableUserInput, GetUserInput, ListUsersInput, ListUsersOutput, RemoveUserFromGroupInput, RemoveUserFromGroupOutput, RevokeSignInSessionsInput, RevokeSignInSessionsOutput, User
from .definitions import CONNECTOR_ID, PROVIDER, ADDED, ADD_USER_TO_GROUP_DEFINITION, ALREADY_EXISTS, CREATED, CREATE_USER_DEFINITION, DEFECT, DISABLED, DISABLE_USER_DEFINITION, ENABLED, ENABLE_USER_DEFINITION, FOUND, GET_USER_DEFINITION, INVALID_RESPONSE, LISTED, LIST_USERS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, REMOVED, REMOVE_USER_FROM_GROUP_DEFINITION, REVOKED, REVOKE_SIGN_IN_SESSIONS_DEFINITION

def get_user_found(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_user_found, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def get_user_not_found(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_user_not_found, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def get_user_provider_rejected(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_user_provider_rejected, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def get_user_invalid_response(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_user_invalid_response, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def get_user_defect(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_user_defect, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def get_user_retry(failure: Failure, after_seconds: float = 0) -> Case[User]:
    return Case.retry(failure, after_seconds)

setattr(get_user_retry, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def get_user_ada_account() -> Case[User]:
    return Case.branch_result(FOUND, User(id='11111111-2222-4333-8444-555555555555', user_principal_name='ada.lovelace@contoso.com', display_name='Ada Lovelace', given_name='Ada', surname='Lovelace', mail_nickname='ada.lovelace', is_account_enabled=True, user_type='Member', department='Engineering', usage_location='GB', created_date_time='2026-10-25T09:00:00Z'))

setattr(get_user_ada_account, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def get_user_account_missing() -> Case[User]:
    return Case.branch_result(NOT_FOUND, User(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getUser", "Account was not found."))

setattr(get_user_account_missing, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def get_user_account_read_forbidden() -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, User(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getUser", "Microsoft Graph rejected the account read."))

setattr(get_user_account_read_forbidden, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def list_users_listed(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_users_listed, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def list_users_provider_rejected(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_users_provider_rejected, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def list_users_invalid_response(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_users_invalid_response, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def list_users_defect(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_users_defect, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def list_users_retry(failure: Failure, after_seconds: float = 0) -> Case[ListUsersOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_users_retry, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def list_users_tenant_accounts() -> Case[ListUsersOutput]:
    return Case.paged(LISTED, (ListUsersOutput(users=(User(id='22222222-3333-4444-8555-666666666666', user_principal_name='grace.hopper@contoso.com', display_name='Grace Hopper', is_account_enabled=True, user_type='Member'),), next_page_token='https://graph.microsoft.com/v1.0/users?$skiptoken=page-2'), ListUsersOutput(users=(User(id='11111111-2222-4333-8444-555555555555', user_principal_name='ada.lovelace@contoso.com', display_name='Ada Lovelace', given_name='Ada', surname='Lovelace', mail_nickname='ada.lovelace', is_account_enabled=True, user_type='Member', department='Engineering', usage_location='GB', created_date_time='2026-10-25T09:00:00Z'), User(id='22222222-3333-4444-8555-666666666666', user_principal_name='grace.hopper@contoso.com', display_name='Grace Hopper', is_account_enabled=True, user_type='Member')), next_page_token='https://graph.microsoft.com/v1.0/users?$skiptoken=page-3'), ListUsersOutput(users=(User(id='33333333-4444-4555-8666-777777777777', user_principal_name='alan.turing@contoso.com', display_name='Alan Turing', is_account_enabled=False, user_type='Member'),))))

setattr(list_users_tenant_accounts, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def list_users_unsupported_account_filter() -> Case[ListUsersOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListUsersOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "listUsers", "Microsoft Graph rejected the user filter."))

setattr(list_users_unsupported_account_filter, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def create_user_created(value: CreateUserOutput, failure: Failure | None = None) -> Case[CreateUserOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_user_created, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def create_user_already_exists(value: CreateUserOutput, failure: Failure | None = None) -> Case[CreateUserOutput]:
    return Case.branch_result(ALREADY_EXISTS, value, failure)

setattr(create_user_already_exists, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def create_user_provider_rejected(value: CreateUserOutput, failure: Failure | None = None) -> Case[CreateUserOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_user_provider_rejected, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def create_user_invalid_response(value: CreateUserOutput, failure: Failure | None = None) -> Case[CreateUserOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_user_invalid_response, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def create_user_defect(value: CreateUserOutput, failure: Failure | None = None) -> Case[CreateUserOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_user_defect, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def create_user_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateUserOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_user_retry, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def create_user_created_ada() -> Case[CreateUserOutput]:
    return Case.branch_result(CREATED, CreateUserOutput(user=User(id='11111111-2222-4333-8444-555555555555', user_principal_name='ada.lovelace@contoso.com', display_name='Ada Lovelace', given_name='Ada', surname='Lovelace', mail_nickname='ada.lovelace', is_account_enabled=True, user_type='Member', department='Engineering', usage_location='GB', created_date_time='2026-10-25T09:00:00Z'), was_already_created=False))

setattr(create_user_created_ada, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def create_user_account_name_taken() -> Case[CreateUserOutput]:
    return Case.branch_result(ALREADY_EXISTS, CreateUserOutput(user=User(id='66666666-7777-4888-8999-aaaaaaaaaaaa', user_principal_name='taken@contoso.com', display_name='Existing Account', mail_nickname='taken', is_account_enabled=True, user_type='Member')), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "createUser", "User principal name belongs to another account."))

setattr(create_user_account_name_taken, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def create_user_unverified_domain() -> Case[CreateUserOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateUserOutput(user=User()), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createUser", "Microsoft Graph rejected the unverified domain."))

setattr(create_user_unverified_domain, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def disable_user_disabled(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(DISABLED, value, failure)

setattr(disable_user_disabled, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def disable_user_not_found(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(disable_user_not_found, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def disable_user_provider_rejected(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(disable_user_provider_rejected, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def disable_user_invalid_response(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(disable_user_invalid_response, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def disable_user_defect(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(DEFECT, value, failure)

setattr(disable_user_defect, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def disable_user_retry(failure: Failure, after_seconds: float = 0) -> Case[User]:
    return Case.retry(failure, after_seconds)

setattr(disable_user_retry, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def disable_user_disabled_ada() -> Case[User]:
    return Case.branch_result(DISABLED, User(id='11111111-2222-4333-8444-555555555555', user_principal_name='ada.lovelace@contoso.com', display_name='Ada Lovelace', given_name='Ada', surname='Lovelace', mail_nickname='ada.lovelace', is_account_enabled=False, user_type='Member', department='Engineering', usage_location='GB', created_date_time='2026-10-25T09:00:00Z'))

setattr(disable_user_disabled_ada, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def disable_user_disable_missing_account() -> Case[User]:
    return Case.branch_result(NOT_FOUND, User(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "disableUser", "Account was not found."))

setattr(disable_user_disable_missing_account, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def disable_user_disable_forbidden() -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, User(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "disableUser", "Microsoft Graph rejected the account change."))

setattr(disable_user_disable_forbidden, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def enable_user_enabled(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(ENABLED, value, failure)

setattr(enable_user_enabled, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def enable_user_not_found(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(enable_user_not_found, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def enable_user_provider_rejected(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(enable_user_provider_rejected, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def enable_user_invalid_response(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(enable_user_invalid_response, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def enable_user_defect(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(DEFECT, value, failure)

setattr(enable_user_defect, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def enable_user_retry(failure: Failure, after_seconds: float = 0) -> Case[User]:
    return Case.retry(failure, after_seconds)

setattr(enable_user_retry, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def enable_user_enabled_ada() -> Case[User]:
    return Case.branch_result(ENABLED, User(id='11111111-2222-4333-8444-555555555555', user_principal_name='ada.lovelace@contoso.com', display_name='Ada Lovelace', given_name='Ada', surname='Lovelace', mail_nickname='ada.lovelace', is_account_enabled=True, user_type='Member', department='Engineering', usage_location='GB', created_date_time='2026-10-25T09:00:00Z'))

setattr(enable_user_enabled_ada, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def enable_user_enable_missing_account() -> Case[User]:
    return Case.branch_result(NOT_FOUND, User(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "enableUser", "Account was not found."))

setattr(enable_user_enable_missing_account, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def enable_user_enable_forbidden() -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, User(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "enableUser", "Microsoft Graph rejected the account change."))

setattr(enable_user_enable_forbidden, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def revoke_sign_in_sessions_revoked(value: RevokeSignInSessionsOutput, failure: Failure | None = None) -> Case[RevokeSignInSessionsOutput]:
    return Case.branch_result(REVOKED, value, failure)

setattr(revoke_sign_in_sessions_revoked, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def revoke_sign_in_sessions_not_found(value: RevokeSignInSessionsOutput, failure: Failure | None = None) -> Case[RevokeSignInSessionsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(revoke_sign_in_sessions_not_found, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def revoke_sign_in_sessions_provider_rejected(value: RevokeSignInSessionsOutput, failure: Failure | None = None) -> Case[RevokeSignInSessionsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(revoke_sign_in_sessions_provider_rejected, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def revoke_sign_in_sessions_invalid_response(value: RevokeSignInSessionsOutput, failure: Failure | None = None) -> Case[RevokeSignInSessionsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(revoke_sign_in_sessions_invalid_response, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def revoke_sign_in_sessions_defect(value: RevokeSignInSessionsOutput, failure: Failure | None = None) -> Case[RevokeSignInSessionsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(revoke_sign_in_sessions_defect, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def revoke_sign_in_sessions_retry(failure: Failure, after_seconds: float = 0) -> Case[RevokeSignInSessionsOutput]:
    return Case.retry(failure, after_seconds)

setattr(revoke_sign_in_sessions_retry, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def revoke_sign_in_sessions_ada_sessions_revoked() -> Case[RevokeSignInSessionsOutput]:
    return Case.branch_result(REVOKED, RevokeSignInSessionsOutput(user_key='11111111-2222-4333-8444-555555555555'))

setattr(revoke_sign_in_sessions_ada_sessions_revoked, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def revoke_sign_in_sessions_revoke_missing_account() -> Case[RevokeSignInSessionsOutput]:
    return Case.branch_result(NOT_FOUND, RevokeSignInSessionsOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "revokeSignInSessions", "Account was not found."))

setattr(revoke_sign_in_sessions_revoke_missing_account, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def revoke_sign_in_sessions_revoke_forbidden() -> Case[RevokeSignInSessionsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, RevokeSignInSessionsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "revokeSignInSessions", "Microsoft Graph rejected session revocation."))

setattr(revoke_sign_in_sessions_revoke_forbidden, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def add_user_to_group_added(value: AddUserToGroupOutput, failure: Failure | None = None) -> Case[AddUserToGroupOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_user_to_group_added, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def add_user_to_group_not_found(value: AddUserToGroupOutput, failure: Failure | None = None) -> Case[AddUserToGroupOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_user_to_group_not_found, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def add_user_to_group_provider_rejected(value: AddUserToGroupOutput, failure: Failure | None = None) -> Case[AddUserToGroupOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_user_to_group_provider_rejected, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def add_user_to_group_invalid_response(value: AddUserToGroupOutput, failure: Failure | None = None) -> Case[AddUserToGroupOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(add_user_to_group_invalid_response, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def add_user_to_group_defect(value: AddUserToGroupOutput, failure: Failure | None = None) -> Case[AddUserToGroupOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_user_to_group_defect, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def add_user_to_group_retry(failure: Failure, after_seconds: float = 0) -> Case[AddUserToGroupOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_user_to_group_retry, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def add_user_to_group_ada_added_to_team() -> Case[AddUserToGroupOutput]:
    return Case.branch_result(ADDED, AddUserToGroupOutput(group_id='aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee', user_id='11111111-2222-4333-8444-555555555555', was_already_member=False))

setattr(add_user_to_group_ada_added_to_team, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def add_user_to_group_add_to_missing_group() -> Case[AddUserToGroupOutput]:
    return Case.branch_result(NOT_FOUND, AddUserToGroupOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addUserToGroup", "Group was not found."))

setattr(add_user_to_group_add_to_missing_group, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def add_user_to_group_dynamic_group_rejects_add() -> Case[AddUserToGroupOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddUserToGroupOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "addUserToGroup", "Microsoft Graph rejected the dynamic group membership."))

setattr(add_user_to_group_dynamic_group_rejects_add, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def remove_user_from_group_removed(value: RemoveUserFromGroupOutput, failure: Failure | None = None) -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(REMOVED, value, failure)

setattr(remove_user_from_group_removed, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def remove_user_from_group_not_found(value: RemoveUserFromGroupOutput, failure: Failure | None = None) -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(remove_user_from_group_not_found, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def remove_user_from_group_provider_rejected(value: RemoveUserFromGroupOutput, failure: Failure | None = None) -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(remove_user_from_group_provider_rejected, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def remove_user_from_group_invalid_response(value: RemoveUserFromGroupOutput, failure: Failure | None = None) -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(remove_user_from_group_invalid_response, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def remove_user_from_group_defect(value: RemoveUserFromGroupOutput, failure: Failure | None = None) -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(remove_user_from_group_defect, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def remove_user_from_group_retry(failure: Failure, after_seconds: float = 0) -> Case[RemoveUserFromGroupOutput]:
    return Case.retry(failure, after_seconds)

setattr(remove_user_from_group_retry, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def remove_user_from_group_ada_removed_from_team() -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(REMOVED, RemoveUserFromGroupOutput(group_id='aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee', user_id='11111111-2222-4333-8444-555555555555', was_already_removed=False))

setattr(remove_user_from_group_ada_removed_from_team, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def remove_user_from_group_remove_from_missing_group() -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(NOT_FOUND, RemoveUserFromGroupOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "removeUserFromGroup", "Group was not found."))

setattr(remove_user_from_group_remove_from_missing_group, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

def remove_user_from_group_dynamic_group_rejects_removal() -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(PROVIDER_REJECTED, RemoveUserFromGroupOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "removeUserFromGroup", "Microsoft Graph rejected the dynamic group membership change."))

setattr(remove_user_from_group_dynamic_group_rejects_removal, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")


class Mock:

    def __init__(self, connection_name: str = "microsoft-entra-id") -> None:
        self.get_user_script: Query[GetUserInput, User] = Query(GET_USER_DEFINITION)
        self.get_user_script.default(get_user_ada_account())
        self.list_users_script: Query[ListUsersInput, ListUsersOutput] = Query(LIST_USERS_DEFINITION)
        self.list_users_script.default(list_users_tenant_accounts())
        self.create_user_script: Mutation[CreateUserInput, CreateUserOutput] = Mutation(CREATE_USER_DEFINITION)
        self.create_user_script.default(create_user_created_ada())
        self.disable_user_script: Mutation[DisableUserInput, User] = Mutation(DISABLE_USER_DEFINITION)
        self.disable_user_script.default(disable_user_disabled_ada())
        self.enable_user_script: Mutation[EnableUserInput, User] = Mutation(ENABLE_USER_DEFINITION)
        self.enable_user_script.default(enable_user_enabled_ada())
        self.revoke_sign_in_sessions_script: Mutation[RevokeSignInSessionsInput, RevokeSignInSessionsOutput] = Mutation(REVOKE_SIGN_IN_SESSIONS_DEFINITION)
        self.revoke_sign_in_sessions_script.default(revoke_sign_in_sessions_ada_sessions_revoked())
        self.add_user_to_group_script: Mutation[AddUserToGroupInput, AddUserToGroupOutput] = Mutation(ADD_USER_TO_GROUP_DEFINITION)
        self.add_user_to_group_script.default(add_user_to_group_ada_added_to_team())
        self.remove_user_from_group_script: Mutation[RemoveUserFromGroupInput, RemoveUserFromGroupOutput] = Mutation(REMOVE_USER_FROM_GROUP_DEFINITION)
        self.remove_user_from_group_script.default(remove_user_from_group_ada_removed_from_team())
        self._connection = new_connection_with_operations(
            get_user=self.get_user_script,
            list_users=self.list_users_script,
            create_user=self.create_user_script,
            disable_user=self.disable_user_script,
            enable_user=self.enable_user_script,
            revoke_sign_in_sessions=self.revoke_sign_in_sessions_script,
            add_user_to_group=self.add_user_to_group_script,
            remove_user_from_group=self.remove_user_from_group_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.microsoft_entra_id.entraidmock")

__all__ = [
    "Mock",
    "get_user_found",
    "get_user_not_found",
    "get_user_provider_rejected",
    "get_user_invalid_response",
    "get_user_defect",
    "get_user_retry",
    "get_user_ada_account",
    "get_user_account_missing",
    "get_user_account_read_forbidden",
    "list_users_listed",
    "list_users_provider_rejected",
    "list_users_invalid_response",
    "list_users_defect",
    "list_users_retry",
    "list_users_tenant_accounts",
    "list_users_unsupported_account_filter",
    "create_user_created",
    "create_user_already_exists",
    "create_user_provider_rejected",
    "create_user_invalid_response",
    "create_user_defect",
    "create_user_retry",
    "create_user_created_ada",
    "create_user_account_name_taken",
    "create_user_unverified_domain",
    "disable_user_disabled",
    "disable_user_not_found",
    "disable_user_provider_rejected",
    "disable_user_invalid_response",
    "disable_user_defect",
    "disable_user_retry",
    "disable_user_disabled_ada",
    "disable_user_disable_missing_account",
    "disable_user_disable_forbidden",
    "enable_user_enabled",
    "enable_user_not_found",
    "enable_user_provider_rejected",
    "enable_user_invalid_response",
    "enable_user_defect",
    "enable_user_retry",
    "enable_user_enabled_ada",
    "enable_user_enable_missing_account",
    "enable_user_enable_forbidden",
    "revoke_sign_in_sessions_revoked",
    "revoke_sign_in_sessions_not_found",
    "revoke_sign_in_sessions_provider_rejected",
    "revoke_sign_in_sessions_invalid_response",
    "revoke_sign_in_sessions_defect",
    "revoke_sign_in_sessions_retry",
    "revoke_sign_in_sessions_ada_sessions_revoked",
    "revoke_sign_in_sessions_revoke_missing_account",
    "revoke_sign_in_sessions_revoke_forbidden",
    "add_user_to_group_added",
    "add_user_to_group_not_found",
    "add_user_to_group_provider_rejected",
    "add_user_to_group_invalid_response",
    "add_user_to_group_defect",
    "add_user_to_group_retry",
    "add_user_to_group_ada_added_to_team",
    "add_user_to_group_add_to_missing_group",
    "add_user_to_group_dynamic_group_rejects_add",
    "remove_user_from_group_removed",
    "remove_user_from_group_not_found",
    "remove_user_from_group_provider_rejected",
    "remove_user_from_group_invalid_response",
    "remove_user_from_group_defect",
    "remove_user_from_group_retry",
    "remove_user_from_group_ada_removed_from_team",
    "remove_user_from_group_remove_from_missing_group",
    "remove_user_from_group_dynamic_group_rejects_removal",
]
