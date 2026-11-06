# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddUserToGroupInput, AddUserToGroupOutput, CreateUserInput, CreateUserOutput, GetUserInput, GroupMembership, ListUsersInput, ListUsersOutput, RemoveUserFromGroupInput, RemoveUserFromGroupOutput, SuspendUserInput, UnsuspendUserInput, User, UserName
from .definitions import CONNECTOR_ID, PROVIDER, ADDED, ADD_USER_TO_GROUP_DEFINITION, ALREADY_EXISTS, CREATED, CREATE_USER_DEFINITION, DEFECT, FOUND, GET_USER_DEFINITION, INVALID_RESPONSE, LISTED, LIST_USERS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, REMOVED, REMOVE_USER_FROM_GROUP_DEFINITION, SUSPENDED, SUSPEND_USER_DEFINITION, UNSUSPENDED, UNSUSPEND_USER_DEFINITION

def get_user_found(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_user_found, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def get_user_not_found(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_user_not_found, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def get_user_provider_rejected(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_user_provider_rejected, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def get_user_invalid_response(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_user_invalid_response, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def get_user_defect(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_user_defect, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def get_user_retry(failure: Failure, after_seconds: float = 0) -> Case[User]:
    return Case.retry(failure, after_seconds)

setattr(get_user_retry, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def get_user_ada_account() -> Case[User]:
    return Case.branch_result(FOUND, User(id='1001', primary_email='ada@example.com', name=UserName(given_name='Ada', family_name='Lovelace', full_name='Ada Lovelace'), org_unit_path='/Engineering', is_suspended=False, is_mailbox_setup=True))

setattr(get_user_ada_account, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def get_user_not_found_failure() -> Case[User]:
    return Case.branch_result(NOT_FOUND, User(name=UserName()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getUser", "No account has this address or ID."))

setattr(get_user_not_found_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def get_user_provider_rejected_failure() -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, User(name=UserName()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getUser", "Google denied the requested administrator action."))

setattr(get_user_provider_rejected_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def list_users_listed(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_users_listed, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def list_users_provider_rejected(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_users_provider_rejected, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def list_users_invalid_response(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_users_invalid_response, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def list_users_defect(value: ListUsersOutput, failure: Failure | None = None) -> Case[ListUsersOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_users_defect, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def list_users_retry(failure: Failure, after_seconds: float = 0) -> Case[ListUsersOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_users_retry, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def list_users_engineering_users() -> Case[ListUsersOutput]:
    return Case.paged(LISTED, (ListUsersOutput(users=(User(id='1001', primary_email='ada@example.com', name=UserName(given_name='Ada', family_name='Lovelace', full_name='Ada Lovelace'), org_unit_path='/Engineering', is_suspended=False, is_mailbox_setup=True),), next_page_token='engineering-page-2'), ListUsersOutput(users=(User(id='1002', primary_email='bob@example.com', name=UserName(given_name='Bob', family_name='Chen', full_name='Bob Chen'), org_unit_path='/Engineering', is_suspended=True, suspension_reason='ADMIN'), User(id='1001', primary_email='ada@example.com', name=UserName(given_name='Ada', family_name='Lovelace', full_name='Ada Lovelace'), org_unit_path='/Engineering', is_suspended=False, is_mailbox_setup=True)), next_page_token='engineering-page-3'), ListUsersOutput(users=(User(id='1003', primary_email='carol@example.com', name=UserName(given_name='Carol', family_name='Diaz', full_name='Carol Diaz'), org_unit_path='/Engineering', is_suspended=False),))))

setattr(list_users_engineering_users, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def list_users_provider_rejected_failure() -> Case[ListUsersOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListUsersOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listUsers", "Google denied the requested administrator action."))

setattr(list_users_provider_rejected_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def create_user_created(value: CreateUserOutput, failure: Failure | None = None) -> Case[CreateUserOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_user_created, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def create_user_already_exists(value: CreateUserOutput, failure: Failure | None = None) -> Case[CreateUserOutput]:
    return Case.branch_result(ALREADY_EXISTS, value, failure)

setattr(create_user_already_exists, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def create_user_provider_rejected(value: CreateUserOutput, failure: Failure | None = None) -> Case[CreateUserOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_user_provider_rejected, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def create_user_invalid_response(value: CreateUserOutput, failure: Failure | None = None) -> Case[CreateUserOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_user_invalid_response, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def create_user_defect(value: CreateUserOutput, failure: Failure | None = None) -> Case[CreateUserOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_user_defect, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def create_user_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateUserOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_user_retry, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def create_user_ada_created() -> Case[CreateUserOutput]:
    return Case.branch_result(CREATED, CreateUserOutput(user=User(id='1001', primary_email='ada@example.com', name=UserName(given_name='Ada', family_name='Lovelace', full_name='Ada Lovelace'), org_unit_path='/Engineering', is_suspended=False, is_mailbox_setup=True), was_already_created=False))

setattr(create_user_ada_created, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def create_user_already_exists_failure() -> Case[CreateUserOutput]:
    return Case.branch_result(ALREADY_EXISTS, CreateUserOutput(user=User(name=UserName())), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "createUser", "The address belongs to another account, alias, or group that this Step did not create."))

setattr(create_user_already_exists_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def create_user_provider_rejected_failure() -> Case[CreateUserOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateUserOutput(user=User(name=UserName())), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createUser", "Google denied the requested administrator action."))

setattr(create_user_provider_rejected_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def suspend_user_suspended(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(SUSPENDED, value, failure)

setattr(suspend_user_suspended, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def suspend_user_not_found(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(suspend_user_not_found, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def suspend_user_provider_rejected(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(suspend_user_provider_rejected, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def suspend_user_invalid_response(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(suspend_user_invalid_response, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def suspend_user_defect(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(DEFECT, value, failure)

setattr(suspend_user_defect, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def suspend_user_retry(failure: Failure, after_seconds: float = 0) -> Case[User]:
    return Case.retry(failure, after_seconds)

setattr(suspend_user_retry, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def suspend_user_ada_suspended() -> Case[User]:
    return Case.branch_result(SUSPENDED, User(id='1001', primary_email='ada@example.com', name=UserName(given_name='Ada', family_name='Lovelace', full_name='Ada Lovelace'), org_unit_path='/Engineering', is_suspended=True, suspension_reason='ADMIN', is_mailbox_setup=True))

setattr(suspend_user_ada_suspended, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def suspend_user_not_found_failure() -> Case[User]:
    return Case.branch_result(NOT_FOUND, User(name=UserName()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "suspendUser", "No account has this address or ID."))

setattr(suspend_user_not_found_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def suspend_user_provider_rejected_failure() -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, User(name=UserName()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "suspendUser", "Google denied the requested administrator action."))

setattr(suspend_user_provider_rejected_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def unsuspend_user_unsuspended(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(UNSUSPENDED, value, failure)

setattr(unsuspend_user_unsuspended, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def unsuspend_user_not_found(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(unsuspend_user_not_found, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def unsuspend_user_provider_rejected(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(unsuspend_user_provider_rejected, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def unsuspend_user_invalid_response(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(unsuspend_user_invalid_response, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def unsuspend_user_defect(value: User, failure: Failure | None = None) -> Case[User]:
    return Case.branch_result(DEFECT, value, failure)

setattr(unsuspend_user_defect, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def unsuspend_user_retry(failure: Failure, after_seconds: float = 0) -> Case[User]:
    return Case.retry(failure, after_seconds)

setattr(unsuspend_user_retry, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def unsuspend_user_ada_unsuspended() -> Case[User]:
    return Case.branch_result(UNSUSPENDED, User(id='1001', primary_email='ada@example.com', name=UserName(given_name='Ada', family_name='Lovelace', full_name='Ada Lovelace'), org_unit_path='/Engineering', is_suspended=False, is_mailbox_setup=True))

setattr(unsuspend_user_ada_unsuspended, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def unsuspend_user_not_found_failure() -> Case[User]:
    return Case.branch_result(NOT_FOUND, User(name=UserName()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "unsuspendUser", "No account has this address or ID."))

setattr(unsuspend_user_not_found_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def unsuspend_user_provider_rejected_failure() -> Case[User]:
    return Case.branch_result(PROVIDER_REJECTED, User(name=UserName()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "unsuspendUser", "Google denied the requested administrator action."))

setattr(unsuspend_user_provider_rejected_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def add_user_to_group_added(value: AddUserToGroupOutput, failure: Failure | None = None) -> Case[AddUserToGroupOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_user_to_group_added, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def add_user_to_group_not_found(value: AddUserToGroupOutput, failure: Failure | None = None) -> Case[AddUserToGroupOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_user_to_group_not_found, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def add_user_to_group_provider_rejected(value: AddUserToGroupOutput, failure: Failure | None = None) -> Case[AddUserToGroupOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_user_to_group_provider_rejected, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def add_user_to_group_invalid_response(value: AddUserToGroupOutput, failure: Failure | None = None) -> Case[AddUserToGroupOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(add_user_to_group_invalid_response, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def add_user_to_group_defect(value: AddUserToGroupOutput, failure: Failure | None = None) -> Case[AddUserToGroupOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_user_to_group_defect, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def add_user_to_group_retry(failure: Failure, after_seconds: float = 0) -> Case[AddUserToGroupOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_user_to_group_retry, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def add_user_to_group_ada_joined_engineering() -> Case[AddUserToGroupOutput]:
    return Case.branch_result(ADDED, AddUserToGroupOutput(membership=GroupMembership(group_key='engineering@example.com', member_id='1001', email='ada@example.com', role='MEMBER', type='USER', status='ACTIVE'), was_already_member=False))

setattr(add_user_to_group_ada_joined_engineering, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def add_user_to_group_not_found_failure() -> Case[AddUserToGroupOutput]:
    return Case.branch_result(NOT_FOUND, AddUserToGroupOutput(membership=GroupMembership(role='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addUserToGroup", "The group or the account does not exist."))

setattr(add_user_to_group_not_found_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def add_user_to_group_provider_rejected_failure() -> Case[AddUserToGroupOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddUserToGroupOutput(membership=GroupMembership(role='')), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "addUserToGroup", "Google denied the requested administrator action."))

setattr(add_user_to_group_provider_rejected_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def remove_user_from_group_removed(value: RemoveUserFromGroupOutput, failure: Failure | None = None) -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(REMOVED, value, failure)

setattr(remove_user_from_group_removed, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def remove_user_from_group_not_found(value: RemoveUserFromGroupOutput, failure: Failure | None = None) -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(remove_user_from_group_not_found, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def remove_user_from_group_provider_rejected(value: RemoveUserFromGroupOutput, failure: Failure | None = None) -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(remove_user_from_group_provider_rejected, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def remove_user_from_group_invalid_response(value: RemoveUserFromGroupOutput, failure: Failure | None = None) -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(remove_user_from_group_invalid_response, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def remove_user_from_group_defect(value: RemoveUserFromGroupOutput, failure: Failure | None = None) -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(remove_user_from_group_defect, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def remove_user_from_group_retry(failure: Failure, after_seconds: float = 0) -> Case[RemoveUserFromGroupOutput]:
    return Case.retry(failure, after_seconds)

setattr(remove_user_from_group_retry, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def remove_user_from_group_ada_left_engineering() -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(REMOVED, RemoveUserFromGroupOutput(group_key='engineering@example.com', member_key='1001', was_already_removed=False))

setattr(remove_user_from_group_ada_left_engineering, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def remove_user_from_group_not_found_failure() -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(NOT_FOUND, RemoveUserFromGroupOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "removeUserFromGroup", "The group does not exist."))

setattr(remove_user_from_group_not_found_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

def remove_user_from_group_provider_rejected_failure() -> Case[RemoveUserFromGroupOutput]:
    return Case.branch_result(PROVIDER_REJECTED, RemoveUserFromGroupOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "removeUserFromGroup", "Google denied the requested administrator action."))

setattr(remove_user_from_group_provider_rejected_failure, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")


class Mock:

    def __init__(self, connection_name: str = "google-workspace-admin") -> None:
        self.get_user_script: Query[GetUserInput, User] = Query(GET_USER_DEFINITION)
        self.get_user_script.default(get_user_ada_account())
        self.list_users_script: Query[ListUsersInput, ListUsersOutput] = Query(LIST_USERS_DEFINITION)
        self.list_users_script.default(list_users_engineering_users())
        self.create_user_script: Mutation[CreateUserInput, CreateUserOutput] = Mutation(CREATE_USER_DEFINITION)
        self.create_user_script.default(create_user_ada_created())
        self.suspend_user_script: Mutation[SuspendUserInput, User] = Mutation(SUSPEND_USER_DEFINITION)
        self.suspend_user_script.default(suspend_user_ada_suspended())
        self.unsuspend_user_script: Mutation[UnsuspendUserInput, User] = Mutation(UNSUSPEND_USER_DEFINITION)
        self.unsuspend_user_script.default(unsuspend_user_ada_unsuspended())
        self.add_user_to_group_script: Mutation[AddUserToGroupInput, AddUserToGroupOutput] = Mutation(ADD_USER_TO_GROUP_DEFINITION)
        self.add_user_to_group_script.default(add_user_to_group_ada_joined_engineering())
        self.remove_user_from_group_script: Mutation[RemoveUserFromGroupInput, RemoveUserFromGroupOutput] = Mutation(REMOVE_USER_FROM_GROUP_DEFINITION)
        self.remove_user_from_group_script.default(remove_user_from_group_ada_left_engineering())
        self._connection = new_connection_with_operations(
            get_user=self.get_user_script,
            list_users=self.list_users_script,
            create_user=self.create_user_script,
            suspend_user=self.suspend_user_script,
            unsuspend_user=self.unsuspend_user_script,
            add_user_to_group=self.add_user_to_group_script,
            remove_user_from_group=self.remove_user_from_group_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_workspace_admin.workspaceadminmock")

__all__ = [
    "Mock",
    "get_user_found",
    "get_user_not_found",
    "get_user_provider_rejected",
    "get_user_invalid_response",
    "get_user_defect",
    "get_user_retry",
    "get_user_ada_account",
    "get_user_not_found_failure",
    "get_user_provider_rejected_failure",
    "list_users_listed",
    "list_users_provider_rejected",
    "list_users_invalid_response",
    "list_users_defect",
    "list_users_retry",
    "list_users_engineering_users",
    "list_users_provider_rejected_failure",
    "create_user_created",
    "create_user_already_exists",
    "create_user_provider_rejected",
    "create_user_invalid_response",
    "create_user_defect",
    "create_user_retry",
    "create_user_ada_created",
    "create_user_already_exists_failure",
    "create_user_provider_rejected_failure",
    "suspend_user_suspended",
    "suspend_user_not_found",
    "suspend_user_provider_rejected",
    "suspend_user_invalid_response",
    "suspend_user_defect",
    "suspend_user_retry",
    "suspend_user_ada_suspended",
    "suspend_user_not_found_failure",
    "suspend_user_provider_rejected_failure",
    "unsuspend_user_unsuspended",
    "unsuspend_user_not_found",
    "unsuspend_user_provider_rejected",
    "unsuspend_user_invalid_response",
    "unsuspend_user_defect",
    "unsuspend_user_retry",
    "unsuspend_user_ada_unsuspended",
    "unsuspend_user_not_found_failure",
    "unsuspend_user_provider_rejected_failure",
    "add_user_to_group_added",
    "add_user_to_group_not_found",
    "add_user_to_group_provider_rejected",
    "add_user_to_group_invalid_response",
    "add_user_to_group_defect",
    "add_user_to_group_retry",
    "add_user_to_group_ada_joined_engineering",
    "add_user_to_group_not_found_failure",
    "add_user_to_group_provider_rejected_failure",
    "remove_user_from_group_removed",
    "remove_user_from_group_not_found",
    "remove_user_from_group_provider_rejected",
    "remove_user_from_group_invalid_response",
    "remove_user_from_group_defect",
    "remove_user_from_group_retry",
    "remove_user_from_group_ada_left_engineering",
    "remove_user_from_group_not_found_failure",
    "remove_user_from_group_provider_rejected_failure",
]
