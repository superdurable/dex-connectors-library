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
    # UserKey is the account's object ID or its user principal name, such as ada.lovelace@contoso.com.
    user_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'userKey'})


setattr(GetUserInput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# User is the bounded account record every user operation returns. It never
# includes passwords, authentication methods, phones, addresses, or extension attributes.
@dataclass(frozen=True, slots=True)
class User:
    # ID is the account's object ID, a GUID; prefer it over the user principal name as a durable key, because sign-in names can change.
    id: str = ''
    # UserPrincipalName is the account's sign-in name, such as ada.lovelace@contoso.com.
    user_principal_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'userPrincipalName'})
    # DisplayName is the address-book name.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})
    # GivenName is the first name.
    given_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'givenName', 'dex_json_omit_default': True})
    # Surname is the family name.
    surname: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Mail is the primary SMTP address, which Microsoft sets once a mailbox exists.
    mail: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # MailNickname is the mail alias.
    mail_nickname: str = _dataclass_field(default='', metadata={'dex_json_name': 'mailNickname', 'dex_json_omit_default': True})
    # IsAccountEnabled reports whether the account can sign in.
    is_account_enabled: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAccountEnabled'})
    # UserType is Member or Guest.
    user_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'userType', 'dex_json_omit_default': True})
    # JobTitle is the job title.
    job_title: str = _dataclass_field(default='', metadata={'dex_json_name': 'jobTitle', 'dex_json_omit_default': True})
    # Department is the department.
    department: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # UsageLocation is the two-letter country code Microsoft requires before licenses are assigned.
    usage_location: str = _dataclass_field(default='', metadata={'dex_json_name': 'usageLocation', 'dex_json_omit_default': True})
    # IsOnPremisesSyncEnabled reports that on-premises Active Directory owns the account, so Graph cannot change most of its properties.
    is_on_premises_sync_enabled: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOnPremisesSyncEnabled'})
    # CreatedDateTime is the creation time exactly as Microsoft reports it, in ISO 8601 UTC; it is empty for some older accounts.
    created_date_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdDateTime', 'dex_json_omit_default': True})
    # SignInSessionsValidFromDateTime is the time before which every refresh token and session cookie is invalid, as Microsoft reports it.
    sign_in_sessions_valid_from_date_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'signInSessionsValidFromDateTime', 'dex_json_omit_default': True})


setattr(User, '__module__', 'dex_connectors.microsoft_entra_id.models')


# ListUsersInput selects one page of accounts in the tenant. Leave every field
# blank to list all accounts in Microsoft Graph's default order.
@dataclass(frozen=True, slots=True)
class ListUsersInput:
    # Filter is an OData $filter expression, such as accountEnabled eq false or startsWith(userPrincipalName,'ada').
    # Operators Microsoft documents as advanced, such as endsWith and ne, also need UsesAdvancedQuery.
    filter: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Search is a $search expression including its double quotes, such as "displayName:Ada"; it always uses
    # Microsoft's eventually consistent index, so a just-created account may be missing.
    search: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # UsesAdvancedQuery sends ConsistencyLevel: eventual with $count=true, which Microsoft requires for advanced
    # filters; results then come from an index that can lag recent changes.
    uses_advanced_query: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'usesAdvancedQuery', 'dex_json_omit_default': True})
    # PageSize is 1 to 999 accounts; zero uses the connection's listUsersPageSize.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is the NextPageToken of the previous page. It carries the original query, so Filter, Search,
    # and PageSize must be blank when it is set; keep UsesAdvancedQuery as it was.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListUsersInput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# ListUsersOutput is one page of accounts.
@dataclass(frozen=True, slots=True)
class ListUsersOutput:
    # Users lists the page's accounts; it may be empty.
    users: tuple[User, ...] = ()
    # NextPageToken selects the following page; empty means this is the last page. It is Microsoft's
    # complete opaque nextLink URL, which Microsoft says not to take apart.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'users', _replace_nil_slices(self.users, 1))


ListUsersOutput.__annotations__['users'] = tuple[User, ...] | None
ListUsersOutput.__dataclass_fields__['users'].type = tuple[User, ...] | None
setattr(ListUsersOutput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# CreateUserInput describes one enabled account to create. The connector generates the
# initial password itself: no password is accepted, returned, stored, or logged.
@dataclass(frozen=True, slots=True)
class CreateUserInput:
    # UserPrincipalName is the new sign-in name in one of the tenant's verified managed domains, such as
    # ada.lovelace@contoso.com: at most 64 letters, digits, and ' . - _ ! # ^ ~ before the @, at most 113 characters in all.
    user_principal_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'userPrincipalName'})
    # DisplayName is the address-book name, at most 256 characters; blank joins GivenName and Surname.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})
    # GivenName is the optional first name, at most 64 characters.
    given_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'givenName', 'dex_json_omit_default': True})
    # Surname is the optional family name, at most 64 characters.
    surname: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # MailNickname is the mail alias, at most 64 characters; blank uses the part of UserPrincipalName before the @.
    mail_nickname: str = _dataclass_field(default='', metadata={'dex_json_name': 'mailNickname', 'dex_json_omit_default': True})
    # JobTitle is the optional job title, at most 128 characters.
    job_title: str = _dataclass_field(default='', metadata={'dex_json_name': 'jobTitle', 'dex_json_omit_default': True})
    # Department is the optional department, at most 64 characters.
    department: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # UsageLocation is an optional two-letter ISO 3166 country code, such as US, which Microsoft requires before
    # a license can be assigned.
    usage_location: str = _dataclass_field(default='', metadata={'dex_json_name': 'usageLocation', 'dex_json_omit_default': True})
    # ProvisioningKey optionally identifies one logical account across Step executions, such as an HR hire ID.
    # It is 1 to 128 letters, digits, and . _ : - characters; blank uses the Step's Call ID.
    provisioning_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'provisioningKey', 'dex_json_omit_default': True})


setattr(CreateUserInput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# CreateUserOutput is the account that holds the requested user principal name.
@dataclass(frozen=True, slots=True)
class CreateUserOutput:
    # User is the created account, or for alreadyExists the other account Microsoft returns for the name, if any.
    user: User = _dataclass_field(default_factory=User)
    # WasAlreadyCreated reports that an earlier attempt or Step execution with the same key created the account.
    was_already_created: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyCreated'})


setattr(CreateUserOutput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# DisableUserInput identifies one account whose sign-in to block.
@dataclass(frozen=True, slots=True)
class DisableUserInput:
    # UserKey is the account's object ID or its user principal name.
    user_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'userKey'})


setattr(DisableUserInput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# EnableUserInput identifies one account whose sign-in to restore.
@dataclass(frozen=True, slots=True)
class EnableUserInput:
    # UserKey is the account's object ID or its user principal name.
    user_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'userKey'})


setattr(EnableUserInput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# RevokeSignInSessionsInput identifies one account whose sign-in sessions to revoke.
@dataclass(frozen=True, slots=True)
class RevokeSignInSessionsInput:
    # UserKey is the account's object ID or its user principal name.
    user_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'userKey'})


setattr(RevokeSignInSessionsInput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# RevokeSignInSessionsOutput confirms that Microsoft accepted the revocation.
@dataclass(frozen=True, slots=True)
class RevokeSignInSessionsOutput:
    # UserKey is the user key the Step requested.
    user_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'userKey'})


setattr(RevokeSignInSessionsOutput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# AddUserToGroupInput adds one account to one group.
@dataclass(frozen=True, slots=True)
class AddUserToGroupInput:
    # GroupID is the group's object ID, a GUID shown at https://entra.microsoft.com > Entra ID > Groups > the group > Overview.
    group_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'groupId'})
    # UserID is the account's object ID, such as the ID createUser or getUser returned.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId'})


setattr(AddUserToGroupInput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# AddUserToGroupOutput confirms that the account is a direct member of the group.
@dataclass(frozen=True, slots=True)
class AddUserToGroupOutput:
    # GroupID is the group's object ID in lowercase.
    group_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'groupId'})
    # UserID is the account's object ID in lowercase.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId'})
    # WasAlreadyMember reports that the account was already a direct member when this attempt ran.
    was_already_member: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyMember'})


setattr(AddUserToGroupOutput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# RemoveUserFromGroupInput removes one account's direct membership from one group.
@dataclass(frozen=True, slots=True)
class RemoveUserFromGroupInput:
    # GroupID is the group's object ID, a GUID.
    group_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'groupId'})
    # UserID is the account's object ID.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId'})


setattr(RemoveUserFromGroupInput, '__module__', 'dex_connectors.microsoft_entra_id.models')


# RemoveUserFromGroupOutput confirms that the account is no longer a direct member of the group.
@dataclass(frozen=True, slots=True)
class RemoveUserFromGroupOutput:
    # GroupID is the group's object ID in lowercase.
    group_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'groupId'})
    # UserID is the account's object ID in lowercase.
    user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'userId'})
    # WasAlreadyRemoved reports that the account was not a direct member when this attempt ran.
    was_already_removed: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyRemoved'})


setattr(RemoveUserFromGroupOutput, '__module__', 'dex_connectors.microsoft_entra_id.models')
