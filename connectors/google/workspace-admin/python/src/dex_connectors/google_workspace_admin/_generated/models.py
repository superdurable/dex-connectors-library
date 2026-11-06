# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# GetUserInput identifies one account to read.
@dataclass(frozen=True, slots=True)
class GetUserInput:
    # UserKey is the account's primary email address, an alias address, or its numeric unique user ID.
    user_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'userKey'})


setattr(GetUserInput, '__module__', 'dex_connectors.google_workspace_admin.models')


# UserName is an account's name.
@dataclass(frozen=True, slots=True)
class UserName:
    # GivenName is the first name.
    given_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'givenName'})
    # FamilyName is the last name.
    family_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'familyName'})
    # FullName is Google's read-only concatenation of the two.
    full_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'fullName', 'dex_json_omit_default': True})


setattr(UserName, '__module__', 'dex_connectors.google_workspace_admin.models')


# User is the bounded account record every user operation returns. It omits
# passwords, recovery details, phones, addresses, and custom schema fields.
@dataclass(frozen=True, slots=True)
class User:
    # ID is Google's stable unique user ID; prefer it over the address as a durable key, because addresses can be renamed.
    id: str = ''
    # PrimaryEmail is the account's sign-in address.
    primary_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'primaryEmail'})
    # Name holds the given, family, and full names.
    name: UserName = _dataclass_field(default_factory=UserName)
    # OrgUnitPath is the account's organizational unit, such as /Engineering; / is the top level.
    org_unit_path: str = _dataclass_field(default='', metadata={'dex_json_name': 'orgUnitPath'})
    # IsSuspended reports whether the account is suspended and cannot sign in.
    is_suspended: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSuspended'})
    # SuspensionReason is Google's reason, such as ADMIN, when IsSuspended is true.
    suspension_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'suspensionReason', 'dex_json_omit_default': True})
    # IsArchived reports whether the account is archived.
    is_archived: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isArchived'})
    # IsAdmin reports super administrator privileges.
    is_admin: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAdmin'})
    # IsDelegatedAdmin reports delegated administrator privileges.
    is_delegated_admin: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDelegatedAdmin'})
    # IsEnrolledIn2SV reports enrollment in 2-Step Verification.
    is_enrolled_in2_sv: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isEnrolledIn2Sv'})
    # IsEnforcedIn2SV reports that 2-Step Verification is enforced for the account.
    is_enforced_in2_sv: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isEnforcedIn2Sv'})
    # ChangePasswordAtNextLogin reports that Google asks for a new password at the next password sign-in.
    change_password_at_next_login: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'changePasswordAtNextLogin'})
    # IsMailboxSetup reports whether Google has finished setting up the Gmail mailbox.
    is_mailbox_setup: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isMailboxSetup'})
    # Aliases lists the account's editable alias addresses.
    aliases: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # CustomerID is the Workspace customer account that owns the user.
    customer_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'customerId', 'dex_json_omit_default': True})
    # CreationTime is the account creation time exactly as Google reports it, normally RFC 3339.
    creation_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'creationTime', 'dex_json_omit_default': True})
    # LastLoginTime is the last sign-in time exactly as Google reports it; Google may report an epoch time for never.
    last_login_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastLoginTime', 'dex_json_omit_default': True})


setattr(User, '__module__', 'dex_connectors.google_workspace_admin.models')


# ListUsersInput selects one page of accounts. Leave both Domain and Customer
# blank to list every account in the administrator's own customer account.
@dataclass(frozen=True, slots=True)
class ListUsersInput:
    # Domain limits the page to one of the account's domains, such as example.com; it cannot be combined with Customer.
    domain: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Customer is a customer ID such as C03az79cb, for a reseller administrator; blank uses the administrator's own account.
    customer: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Query is Google's user search syntax, such as orgUnitPath='/Sales' isSuspended=false; blank lists every account.
    # Google documents that new data can take up to 36 hours to appear in search results.
    query: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # OrderBy is email, givenName, or familyName; blank sorts by primary address.
    order_by: str = _dataclass_field(default='', metadata={'dex_json_name': 'orderBy', 'dex_json_omit_default': True})
    # PageSize is 1 to 500 accounts; zero uses the connection's listUsersPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is the NextPageToken of the previous page; Google keeps it valid for three days.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListUsersInput, '__module__', 'dex_connectors.google_workspace_admin.models')


# ListUsersOutput is one page of accounts.
@dataclass(frozen=True, slots=True)
class ListUsersOutput:
    # Users lists the page's accounts in the requested order; it may be empty.
    users: tuple[User, ...] = ()
    # NextPageToken selects the following page; empty means this is the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'users', _replace_nil_slices(self.users, 1))


ListUsersOutput.__annotations__['users'] = tuple[User, ...] | None
ListUsersOutput.__dataclass_fields__['users'].type = tuple[User, ...] | None
setattr(ListUsersOutput, '__module__', 'dex_connectors.google_workspace_admin.models')


# CreateUserInput describes one account to create. The connector generates the
# initial password itself: no password is accepted, returned, stored, or logged.
@dataclass(frozen=True, slots=True)
class CreateUserInput:
    # PrimaryEmail is the new sign-in address in one of the account's verified domains, such as ada@example.com.
    primary_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'primaryEmail'})
    # GivenName is the required first name, at most 60 characters.
    given_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'givenName'})
    # FamilyName is the required last name, at most 60 characters.
    family_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'familyName'})
    # OrgUnitPath places the account in an organizational unit, such as /Engineering; blank uses the top-level unit /.
    org_unit_path: str = _dataclass_field(default='', metadata={'dex_json_name': 'orgUnitPath', 'dex_json_omit_default': True})
    # RecoveryEmail is an optional personal address Google can use for account recovery.
    recovery_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'recoveryEmail', 'dex_json_omit_default': True})
    # RecoveryPhone is an optional E.164 recovery phone number, such as +16506661212.
    recovery_phone: str = _dataclass_field(default='', metadata={'dex_json_name': 'recoveryPhone', 'dex_json_omit_default': True})
    # ProvisioningKey optionally identifies one logical account across Step executions, such as an HR hire ID.
    # It is 1 to 128 letters, digits, and . _ : - characters; blank uses the Step's Call ID.
    provisioning_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'provisioningKey', 'dex_json_omit_default': True})


setattr(CreateUserInput, '__module__', 'dex_connectors.google_workspace_admin.models')


# CreateUserOutput is the account that holds the requested address.
@dataclass(frozen=True, slots=True)
class CreateUserOutput:
    # User is the created account, or for alreadyExists the other account Google returns for the address, if any.
    user: User = _dataclass_field(default_factory=User)
    # WasAlreadyCreated reports that an earlier attempt or Step execution with the same key created the account.
    was_already_created: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyCreated'})


setattr(CreateUserOutput, '__module__', 'dex_connectors.google_workspace_admin.models')


# SuspendUserInput identifies one account to suspend.
@dataclass(frozen=True, slots=True)
class SuspendUserInput:
    # UserKey is the account's primary email address, an alias address, or its numeric unique user ID.
    user_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'userKey'})


setattr(SuspendUserInput, '__module__', 'dex_connectors.google_workspace_admin.models')


# UnsuspendUserInput identifies one account whose sign-in to restore.
@dataclass(frozen=True, slots=True)
class UnsuspendUserInput:
    # UserKey is the account's primary email address, an alias address, or its numeric unique user ID.
    user_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'userKey'})


setattr(UnsuspendUserInput, '__module__', 'dex_connectors.google_workspace_admin.models')


# AddUserToGroupInput adds one account to one group.
@dataclass(frozen=True, slots=True)
class AddUserToGroupInput:
    # GroupKey is the group's email address, an alias, or its unique group ID.
    group_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'groupKey'})
    # MemberEmail is the account's primary or alias email address.
    member_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'memberEmail'})
    # Role is MEMBER, MANAGER, or OWNER; blank adds the account as MEMBER.
    role: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(AddUserToGroupInput, '__module__', 'dex_connectors.google_workspace_admin.models')


# GroupMembership is one direct member of a group.
@dataclass(frozen=True, slots=True)
class GroupMembership:
    # GroupKey is the group key the Step requested.
    group_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'groupKey'})
    # MemberID is the member's unique ID; for an account it is the user ID.
    member_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'memberId'})
    # Email is the member's address as Google reports it.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Role is the member's role, which may differ from the requested role when WasAlreadyMember is true.
    role: str = _dataclass_field(kw_only=True)
    # Type is USER, GROUP, CUSTOMER, or EXTERNAL.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Status is Google's membership status, such as ACTIVE, when reported.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(GroupMembership, '__module__', 'dex_connectors.google_workspace_admin.models')


# AddUserToGroupOutput is the account's direct membership.
@dataclass(frozen=True, slots=True)
class AddUserToGroupOutput:
    # Membership is the membership Google returned or read back.
    membership: GroupMembership
    # WasAlreadyMember reports that the account was already a direct member; its existing role is unchanged.
    was_already_member: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyMember'})


setattr(AddUserToGroupOutput, '__module__', 'dex_connectors.google_workspace_admin.models')


# RemoveUserFromGroupInput removes one account's direct membership from one group.
@dataclass(frozen=True, slots=True)
class RemoveUserFromGroupInput:
    # GroupKey is the group's email address, an alias, or its unique group ID.
    group_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'groupKey'})
    # MemberKey is the account's primary or alias email address or its numeric unique user ID.
    member_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'memberKey'})


setattr(RemoveUserFromGroupInput, '__module__', 'dex_connectors.google_workspace_admin.models')


# RemoveUserFromGroupOutput confirms the account is no longer a direct member.
@dataclass(frozen=True, slots=True)
class RemoveUserFromGroupOutput:
    # GroupKey is the group key the Step requested.
    group_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'groupKey'})
    # MemberKey is the member key the Step requested.
    member_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'memberKey'})
    # WasAlreadyRemoved reports that the account was not a direct member when this attempt ran.
    was_already_removed: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyRemoved'})


setattr(RemoveUserFromGroupOutput, '__module__', 'dex_connectors.google_workspace_admin.models')
