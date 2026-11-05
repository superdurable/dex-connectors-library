# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go-compatible Dex invocations for Google Workspace Directory operations."""

import base64
import json
import re
import secrets
import unicodedata
from datetime import UTC, datetime
from email.utils import parseaddr
from typing import Any

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)

from ._generated.definitions import (
    ADD_USER_TO_GROUP_DEFINITION,
    CREATE_USER_DEFINITION,
    GET_USER_DEFINITION,
    LIST_USERS_DEFINITION,
    REMOVE_USER_FROM_GROUP_DEFINITION,
    SUSPEND_USER_DEFINITION,
    UNSUSPEND_USER_DEFINITION,
)
from ._generated.models import (
    AddUserToGroupInput,
    AddUserToGroupOutput,
    CreateUserInput,
    CreateUserOutput,
    GetUserInput,
    GroupMembership,
    ListUsersInput,
    ListUsersOutput,
    RemoveUserFromGroupInput,
    RemoveUserFromGroupOutput,
    User,
    UserName,
)
from .client import Client, Exchange, path_escape

_USER_ID = re.compile(r"[0-9]{1,64}\Z")
_GROUP_ID = re.compile(r"[0-9a-z]{1,64}\Z")
_CREATION_KEY = re.compile(r"[A-Za-z0-9._:-]{1,128}\Z")
_PHONE = re.compile(r"\+[1-9][0-9]{1,14}\Z")
_CUSTOMER = re.compile(r"C[0-9A-Za-z]{1,32}\Z")
_DOMAIN_LABEL = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\Z", re.I)
_PAGE_TOKEN = re.compile(r"[A-Za-z0-9._~+/=-]+\Z")


class _WrongType(ValueError):
    """Provider field has a JSON type Go would reject during unmarshal."""


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "google-workspace-admin", operation, message)


def _bare_email(value: str) -> bool:
    if (
        not value
        or len(value.encode()) > 254
        or any(character in '<>"()[]\\,; \t\r\n/?#%' for character in value)
    ):
        return False
    if value.count("@") != 1:
        return False
    local, domain = value.split("@")
    if any(not part for part in local.split(".") + domain.split(".")):
        return False
    name, address = parseaddr(value)
    return not name and address == value


def _user_key(value: str) -> str:
    key = value.strip()
    if not (_USER_ID.fullmatch(key) or _bare_email(key)):
        raise ValueError("userKey must be one bare email address or a numeric user ID")
    return key


def _group_key(value: str) -> str:
    key = value.strip()
    if not (_GROUP_ID.fullmatch(key) or _bare_email(key)):
        raise ValueError("groupKey must be one bare group email address or a group ID")
    return key


def _string(resource: dict[str, Any], name: str) -> str:
    value = resource.get(name, "")
    if not isinstance(value, str):
        raise _WrongType("invalid field type")
    return value


def _boolean(resource: dict[str, Any], name: str) -> bool:
    value = resource.get(name, False)
    if not isinstance(value, bool):
        raise _WrongType("invalid field type")
    return value


def _object(body: bytes) -> dict[str, Any]:
    try:
        value = json.loads(body)
    except (ValueError, UnicodeDecodeError) as error:
        raise ValueError("not valid JSON") from error
    if not isinstance(value, dict):
        raise ValueError("not valid JSON")
    return value


def _user(resource: dict[str, Any]) -> User:
    identifier, email = _string(resource, "id"), _string(resource, "primaryEmail")
    if not _USER_ID.fullmatch(identifier):
        raise ValueError("account lacks a valid unique ID")
    if not _bare_email(email):
        raise ValueError("account lacks a valid primary address")
    name = resource.get("name")
    if name is None:
        name = {}
    if not isinstance(name, dict):
        raise _WrongType("invalid field type")
    aliases = resource.get("aliases")
    if aliases is None:
        aliases = []
    if not isinstance(aliases, list) or any(not isinstance(alias, str) for alias in aliases):
        raise _WrongType("invalid field type")
    return User(
        id=identifier,
        primary_email=email,
        name=UserName(_string(name, "givenName"), _string(name, "familyName"), _string(name, "fullName")),
        org_unit_path=_string(resource, "orgUnitPath"),
        is_suspended=_boolean(resource, "suspended"),
        suspension_reason=_string(resource, "suspensionReason"),
        is_archived=_boolean(resource, "archived"),
        is_admin=_boolean(resource, "isAdmin"),
        is_delegated_admin=_boolean(resource, "isDelegatedAdmin"),
        is_enrolled_in2_sv=_boolean(resource, "isEnrolledIn2Sv"),
        is_enforced_in2_sv=_boolean(resource, "isEnforcedIn2Sv"),
        change_password_at_next_login=_boolean(resource, "changePasswordAtNextLogin"),
        is_mailbox_setup=_boolean(resource, "isMailboxSetup"),
        aliases=tuple(aliases),
        customer_id=_string(resource, "customerId"),
        creation_time=_string(resource, "creationTime"),
        last_login_time=_string(resource, "lastLoginTime"),
    )


def _decoded_user(body: bytes) -> tuple[User, dict[str, Any]]:
    """Apply Go's JSON type checks before account identity validation."""
    try:
        resource = _object(body)
        return _user(resource), resource
    except _WrongType as error:
        raise ValueError("account is not valid JSON") from error
    except ValueError as error:
        if str(error) == "not valid JSON":
            raise ValueError("account is not valid JSON") from error
        raise


def _membership(body: bytes, group_key: str) -> GroupMembership:
    try:
        resource = _object(body)
        identifier, role = _string(resource, "id"), _string(resource, "role")
        email = _string(resource, "email")
        membership_type = _string(resource, "type")
        status = _string(resource, "status")
    except ValueError as error:
        raise ValueError("membership is not valid JSON") from error
    if not identifier or len(identifier.encode()) > 64 or role not in ("MEMBER", "MANAGER", "OWNER"):
        raise ValueError("membership lacks a valid member ID or role")
    return GroupMembership(
        group_key=group_key,
        member_id=identifier,
        email=email,
        role=role,
        type=membership_type,
        status=status,
    )


def _query_exchange(
    operation: str,
    exchange: Exchange,
    call: Call,
    zero: Any,
    *,
    not_found: str = "providerRejected",
    conflict: str = "providerRejected",
) -> QueryAttempt[Any]:
    if exchange.outcome == "retry":
        return QueryAttempt.retry(exchange.failure(operation), exchange.retry_after)
    branch = {
        "notFound": not_found,
        "conflict": conflict,
        "rejected": "providerRejected",
        "invalidResponse": "invalidResponse",
        "defect": "defect",
    }[exchange.outcome]
    return QueryAttempt.branch_result(branch, zero, exchange.receipt(call), exchange.failure(operation))


def _mutation_exchange(
    operation: str, exchange: Exchange, call: Call, zero: Any, *, not_found: str = "notFound"
) -> MutationAttempt[Any]:
    if exchange.outcome == "retry":
        return MutationAttempt.retry(exchange.failure(operation), exchange.retry_after)
    branch = {
        "notFound": not_found,
        "conflict": "providerRejected",
        "rejected": "providerRejected",
        "invalidResponse": "invalidResponse",
        "defect": "defect",
    }[exchange.outcome]
    return MutationAttempt.branch_result(branch, zero, exchange.receipt(call), exchange.failure(operation))


class _Operation:
    """A provider-bound operation with shared credential failure handling."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Google Workspace Admin client is required")
        self.client = client

    async def credential(self, call: Call) -> Any:
        """Resolve the selected auth method through generated refresh wiring."""
        return await self.client.resolve_credentials(call)


class GetUserOperation(_Operation):
    """Read one Workspace user."""

    def definition(self) -> QueryDefinition:
        return GET_USER_DEFINITION

    async def invoke(self, call: Call, input: GetUserInput) -> QueryAttempt[User]:
        """Validate an account key and classify one users.get exchange."""
        try:
            key = _user_key(input.user_key)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", User(), Receipt(), _failure("getUser", FailureKind.VALIDATION, str(error))
            )
        try:
            credential = await self.credential(call)
        except Exception:
            return QueryAttempt.branch_result(
                "defect",
                User(),
                Receipt(),
                _failure("getUser", FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        exchange = await self.client.exchange(call, credential, "GET", "/users/" + path_escape(key))
        if exchange.outcome != "success":
            return _query_exchange("getUser", exchange, call, User(), not_found="notFound")
        try:
            user, _ = _decoded_user(exchange.body)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                User(),
                exchange.receipt(call),
                _failure(
                    "getUser", FailureKind.PROTOCOL, "provider returned an invalid account: " + str(error)
                ),
            )
        return QueryAttempt.branch_result("found", user, exchange.receipt(call, user.id))


class ListUsersOperation(_Operation):
    """List one bounded Directory page."""

    def definition(self) -> QueryDefinition:
        return LIST_USERS_DEFINITION

    async def invoke(self, call: Call, input: ListUsersInput) -> QueryAttempt[ListUsersOutput]:
        """Build Go-compatible query parameters and decode a user page."""
        try:
            domain, customer = input.domain.strip(), input.customer.strip()
            if domain and customer:
                raise ValueError("set domain or customer, not both")
            if domain:
                labels = domain.split(".")
                if (
                    len(domain.encode()) > 253
                    or len(labels) < 2
                    or any(not _DOMAIN_LABEL.fullmatch(label) for label in labels)
                ):
                    raise ValueError("domain must be a domain name such as example.com")
                query = {"domain": domain}
            elif customer:
                if customer != "my_customer" and not _CUSTOMER.fullmatch(customer):
                    raise ValueError("customer must be a customer ID such as C03az79cb or my_customer")
                query = {"customer": customer}
            else:
                query = {"customer": "my_customer"}
            search = input.query.strip()
            if search:
                if len(search.encode()) > 2048 or any(
                    ord(character) < 32 or ord(character) == 127 for character in search
                ):
                    raise ValueError("query must be at most 2048 bytes without control characters")
                query["query"] = search
            order = input.order_by.strip() or "email"
            if order not in ("email", "givenName", "familyName"):
                raise ValueError("orderBy must be email, givenName, or familyName")
            query["orderBy"] = order
            size = input.page_size or self.client.list_users_page_size
            if not 1 <= size <= 500:
                raise ValueError("pageSize must be from 1 to 500")
            query["maxResults"] = str(size)
            token = input.page_token.strip()
            if token:
                if len(token.encode()) > 4096 or not _PAGE_TOKEN.fullmatch(token):
                    raise ValueError("pageToken must be the NextPageToken of a previous page")
                query["pageToken"] = token
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                ListUsersOutput(),
                Receipt(),
                _failure("listUsers", FailureKind.VALIDATION, str(error)),
            )
        try:
            credential = await self.credential(call)
        except Exception:
            return QueryAttempt.branch_result(
                "defect",
                ListUsersOutput(),
                Receipt(),
                _failure("listUsers", FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        exchange = await self.client.exchange(call, credential, "GET", "/users", query)
        if exchange.outcome != "success":
            return _query_exchange("listUsers", exchange, call, ListUsersOutput())
        try:
            page = _object(exchange.body)
            raw_users = page.get("users")
            if raw_users is None:
                raw_users = []
            if not isinstance(raw_users, list):
                raise _WrongType("invalid field type")
            users_list: list[User] = []
            for index, resource in enumerate(raw_users):
                if not isinstance(resource, dict):
                    raise _WrongType("invalid field type")
                try:
                    users_list.append(_user(resource))
                except _WrongType:
                    raise
                except ValueError as error:
                    raise ValueError(f"account {index}: {error}") from error
            users = tuple(users_list)
            next_token = _string(page, "nextPageToken")
            output = ListUsersOutput(users, next_token)
        except (ValueError, TypeError) as error:
            is_invalid_json = isinstance(error, (_WrongType, TypeError)) or str(error) == "not valid JSON"
            message = "page is not valid JSON" if is_invalid_json else str(error)
            return QueryAttempt.branch_result(
                "invalidResponse",
                ListUsersOutput(),
                exchange.receipt(call),
                _failure("listUsers", FailureKind.PROTOCOL, "provider returned an invalid page: " + message),
            )
        return QueryAttempt.branch_result("listed", output, exchange.receipt(call))


def _display_name(field: str, value: str) -> str:
    name = value.strip()
    if not name:
        raise ValueError(field + " is required")
    if len(name) > 60:
        raise ValueError(field + " must be valid text of at most 60 characters")
    if any(unicodedata.category(character) == "Cc" for character in name):
        raise ValueError(field + " cannot contain control characters")
    return name


class CreateUserOperation(_Operation):
    """Create a user with a stable external creation key and conflict read-back."""

    def definition(self) -> MutationDefinition:
        return CREATE_USER_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateUserInput) -> IdempotencyKey:
        """Use a supplied provisioning key or the Dex Call ID."""
        return IdempotencyKey(input.provisioning_key.strip() or call_id)

    async def invoke(self, call: Call, input: CreateUserInput) -> MutationAttempt[CreateUserOutput]:
        """Insert a new account and converge on the same user after a conflict."""
        operation = "createUser"
        key = str(call.idempotency_key)
        try:
            email = input.primary_email.strip()
            if not _bare_email(email):
                raise ValueError("primaryEmail must be one bare email address")
            if not _CREATION_KEY.fullmatch(key):
                raise ValueError("provisioningKey must be 1 to 128 letters, digits, or . _ : - characters")
            given = _display_name("givenName", input.given_name)
            family = _display_name("familyName", input.family_name)
            org_unit = input.org_unit_path.strip() or "/"
            if (
                not org_unit.startswith("/")
                or len(org_unit.encode()) > 1024
                or any(ord(c) < 32 or ord(c) == 127 for c in org_unit)
            ):
                raise ValueError("orgUnitPath must be a full path such as /Engineering")
            recovery_email = input.recovery_email.strip()
            if recovery_email and not _bare_email(recovery_email):
                raise ValueError("recoveryEmail must be one bare email address")
            recovery_phone = input.recovery_phone.strip()
            if recovery_phone and not _PHONE.fullmatch(recovery_phone):
                raise ValueError("recoveryPhone must be an E.164 number such as +16506661212")
            payload: dict[str, Any] = {
                "primaryEmail": email,
                "name": {"givenName": given, "familyName": family},
                "password": base64.urlsafe_b64encode(secrets.token_bytes(32)).rstrip(b"=").decode(),
                "changePasswordAtNextLogin": True,
                "orgUnitPath": org_unit,
                "externalIds": [{"type": "custom", "customType": "dexIdempotencyKey", "value": key}],
            }
            if recovery_email:
                payload["recoveryEmail"] = recovery_email
            if recovery_phone:
                payload["recoveryPhone"] = recovery_phone
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                CreateUserOutput(),
                Receipt(),
                _failure(operation, FailureKind.VALIDATION, str(error)),
            )
        try:
            credential = await self.credential(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                CreateUserOutput(),
                Receipt(),
                _failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        exchange = await self.client.exchange(call, credential, "POST", "/users", payload=payload)
        if exchange.outcome == "success":
            try:
                user, _ = _decoded_user(exchange.body)
                if user.primary_email.lower() != email.lower():
                    raise ValueError("response account differs")
            except ValueError:
                return MutationAttempt.retry(
                    _failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "insert response is unusable; the retry reads the account back",
                    )
                )
            return MutationAttempt.branch_result(
                "created", CreateUserOutput(user), exchange.receipt(call, user.id)
            )
        if exchange.outcome == "conflict":
            read = await self.client.exchange(call, credential, "GET", "/users/" + path_escape(email))
            if read.outcome == "success":
                try:
                    user, resource = _decoded_user(read.body)
                except ValueError as error:
                    invalid = Exchange(
                        "invalidResponse",
                        read.status,
                        read.headers,
                        read.body,
                        FailureKind.PROTOCOL,
                        "provider returned an invalid account: " + str(error),
                    )
                    return _mutation_exchange(
                        operation, invalid, call, CreateUserOutput(), not_found="providerRejected"
                    )
                ids = resource.get("externalIds")
                matched = isinstance(ids, list) and any(
                    isinstance(item, dict)
                    and item.get("type") == "custom"
                    and item.get("customType") == "dexIdempotencyKey"
                    and item.get("value") == key
                    for item in ids
                )
                if matched:
                    return MutationAttempt.branch_result(
                        "created", CreateUserOutput(user, True), read.receipt(call, user.id)
                    )
                return MutationAttempt.branch_result(
                    "alreadyExists",
                    CreateUserOutput(user),
                    read.receipt(call, user.id),
                    _failure(
                        operation,
                        FailureKind.CONFLICT,
                        "address belongs to an account this Step did not create",
                    ),
                )
            if read.outcome == "notFound":
                context = call.context
                first_attempt = getattr(context, "first_attempt_at", None) if context else None
                if (
                    isinstance(first_attempt, datetime)
                    and (datetime.now(UTC) - first_attempt).total_seconds() < 60
                ):
                    return MutationAttempt.retry(
                        _failure(
                            operation,
                            FailureKind.AVAILABILITY,
                            "Google reports the address as taken but does not return the account yet",
                        )
                    )
                return MutationAttempt.branch_result(
                    "alreadyExists",
                    CreateUserOutput(),
                    read.receipt(call),
                    _failure(
                        operation,
                        FailureKind.CONFLICT,
                        "address belongs to a group or an account outside the directory",
                    ),
                )
            return _mutation_exchange(operation, read, call, CreateUserOutput(), not_found="providerRejected")
        if exchange.outcome == "invalidResponse":
            return MutationAttempt.retry(
                _failure(
                    operation,
                    exchange.kind or FailureKind.RESPONSE_TOO_LARGE,
                    "insert response is unusable; the retry reads the account back",
                )
            )
        return _mutation_exchange(operation, exchange, call, CreateUserOutput(), not_found="providerRejected")


class _SuspensionOperation(_Operation):
    """Apply an absolute account suspension flag."""

    operation_name: str
    success_branch: str
    suspended: bool

    def idempotency_key(self, call_id: CallID, input: Any) -> IdempotencyKey:
        """Use the stable Dex Call ID for a safe absolute update."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: Any) -> MutationAttempt[User]:
        """Set suspended and reject an account returned in the opposite state."""
        try:
            key = _user_key(input.user_key)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", User(), Receipt(), _failure(self.operation_name, FailureKind.VALIDATION, str(error))
            )
        try:
            credential = await self.credential(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                User(),
                Receipt(),
                _failure(
                    self.operation_name, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
                ),
            )
        exchange = await self.client.exchange(
            call, credential, "PUT", "/users/" + path_escape(key), payload={"suspended": self.suspended}
        )
        if exchange.outcome != "success":
            return _mutation_exchange(self.operation_name, exchange, call, User())
        try:
            user, _ = _decoded_user(exchange.body)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "invalidResponse",
                User(),
                exchange.receipt(call),
                _failure(
                    self.operation_name,
                    FailureKind.PROTOCOL,
                    "provider returned an invalid account: " + str(error),
                ),
            )
        receipt = exchange.receipt(call, user.id)
        if user.is_suspended != self.suspended:
            return MutationAttempt.branch_result(
                "providerRejected",
                user,
                receipt,
                _failure(
                    self.operation_name,
                    FailureKind.PROVIDER_REJECTION,
                    "Google returned the account without the requested suspension state",
                ),
            )
        return MutationAttempt.branch_result(self.success_branch, user, receipt)


class SuspendUserOperation(_SuspensionOperation):
    """Set a user account to suspended."""

    operation_name = "suspendUser"
    success_branch = "suspended"
    suspended = True

    def definition(self) -> MutationDefinition:
        return SUSPEND_USER_DEFINITION


class UnsuspendUserOperation(_SuspensionOperation):
    """Restore a user account to active."""

    operation_name = "unsuspendUser"
    success_branch = "unsuspended"
    suspended = False

    def definition(self) -> MutationDefinition:
        return UNSUSPEND_USER_DEFINITION


class AddUserToGroupOperation(_Operation):
    """Add a direct group membership, reading it back after a conflict."""

    def definition(self) -> MutationDefinition:
        return ADD_USER_TO_GROUP_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddUserToGroupInput) -> IdempotencyKey:
        """Use the Dex Call ID for a convergent membership insert."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: AddUserToGroupInput) -> MutationAttempt[AddUserToGroupOutput]:
        """Insert the membership or return its existing role after a conflict."""
        operation = "addUserToGroup"
        try:
            group = _group_key(input.group_key)
            email = input.member_email.strip()
            if not _bare_email(email):
                raise ValueError("memberEmail must be one bare email address")
            role = input.role or "MEMBER"
            if role not in ("MEMBER", "MANAGER", "OWNER"):
                raise ValueError("role must be MEMBER, MANAGER, or OWNER")
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                AddUserToGroupOutput(GroupMembership(role="")),
                Receipt(),
                _failure(operation, FailureKind.VALIDATION, str(error)),
            )
        try:
            credential = await self.credential(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                AddUserToGroupOutput(GroupMembership(role="")),
                Receipt(),
                _failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        path = "/groups/" + path_escape(group) + "/members"
        exchange = await self.client.exchange(
            call, credential, "POST", path, payload={"email": email, "role": role}
        )
        if exchange.outcome == "success":
            try:
                membership = _membership(exchange.body, group)
            except ValueError:
                return MutationAttempt.retry(
                    _failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "insert response is unusable; the retry reads the membership back",
                    )
                )
            return MutationAttempt.branch_result(
                "added", AddUserToGroupOutput(membership), exchange.receipt(call, membership.member_id)
            )
        if exchange.outcome == "conflict":
            read = await self.client.exchange(call, credential, "GET", path + "/" + path_escape(email))
            if read.outcome == "success":
                try:
                    membership = _membership(read.body, group)
                except ValueError as error:
                    return MutationAttempt.branch_result(
                        "invalidResponse",
                        AddUserToGroupOutput(GroupMembership(role="")),
                        read.receipt(call),
                        _failure(
                            operation,
                            FailureKind.PROTOCOL,
                            "provider returned an invalid membership: " + str(error),
                        ),
                    )
                return MutationAttempt.branch_result(
                    "added", AddUserToGroupOutput(membership, True), read.receipt(call, membership.member_id)
                )
            if read.outcome == "notFound":
                return MutationAttempt.retry(
                    _failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "Google reports the membership as existing but does not return it yet",
                    )
                )
            return _mutation_exchange(operation, read, call, AddUserToGroupOutput(GroupMembership(role="")))
        if exchange.outcome == "invalidResponse":
            return MutationAttempt.retry(
                _failure(
                    operation,
                    exchange.kind or FailureKind.RESPONSE_TOO_LARGE,
                    "insert response is unusable; the retry reads the membership back",
                )
            )
        return _mutation_exchange(operation, exchange, call, AddUserToGroupOutput(GroupMembership(role="")))


class RemoveUserFromGroupOperation(_Operation):
    """Remove one direct group membership and confirm a 404 against the group."""

    def definition(self) -> MutationDefinition:
        return REMOVE_USER_FROM_GROUP_DEFINITION

    def idempotency_key(self, call_id: CallID, input: RemoveUserFromGroupInput) -> IdempotencyKey:
        """Use the Dex Call ID for an idempotent direct-membership delete."""
        return IdempotencyKey(call_id)

    async def invoke(
        self, call: Call, input: RemoveUserFromGroupInput
    ) -> MutationAttempt[RemoveUserFromGroupOutput]:
        """Remove the member or distinguish an absent group from an absent member."""
        operation = "removeUserFromGroup"
        try:
            group = _group_key(input.group_key)
            try:
                member = _user_key(input.member_key)
            except ValueError as error:
                raise ValueError("memberKey must be one bare email address or a numeric user ID") from error
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                RemoveUserFromGroupOutput(),
                Receipt(),
                _failure(operation, FailureKind.VALIDATION, str(error)),
            )
        try:
            credential = await self.credential(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                RemoveUserFromGroupOutput(),
                Receipt(),
                _failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        output = RemoveUserFromGroupOutput(group, member)
        path = "/groups/" + path_escape(group) + "/members"
        exchange = await self.client.exchange(call, credential, "DELETE", path + "/" + path_escape(member))
        if exchange.outcome == "success":
            return MutationAttempt.branch_result("removed", output, exchange.receipt(call))
        if exchange.outcome == "invalidResponse":
            return MutationAttempt.retry(
                _failure(
                    operation,
                    exchange.kind or FailureKind.RESPONSE_TOO_LARGE,
                    "delete response is unusable; the retry confirms the removal",
                )
            )
        if exchange.outcome == "notFound":
            check = await self.client.exchange(call, credential, "GET", path, {"maxResults": "1"})
            if check.outcome != "success":
                return _mutation_exchange(operation, check, call, RemoveUserFromGroupOutput())
            return MutationAttempt.branch_result(
                "removed", RemoveUserFromGroupOutput(group, member, True), check.receipt(call)
            )
        return _mutation_exchange(operation, exchange, call, RemoveUserFromGroupOutput())
