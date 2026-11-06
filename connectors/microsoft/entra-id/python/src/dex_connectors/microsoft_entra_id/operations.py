# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Microsoft Entra ID Query and Mutation invocations."""

from __future__ import annotations

import json
import re
import secrets
import unicodedata
from dataclasses import replace
from typing import Any
from urllib.parse import parse_qs, urlsplit

from dex_connectors.sdk import (
    Call,
    CallID,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    QueryAttempt,
    Receipt,
)

from ._generated import definitions as definitions
from .client import (
    CREATION_SELECT,
    GRAPH_BASE,
    USER_SELECT,
    Client,
    Exchange,
    convert_user,
    decode_user,
    failure,
    is_lookup_upn,
    path_escape,
)
from .models import (
    AddUserToGroupInput,
    AddUserToGroupOutput,
    CreateUserInput,
    CreateUserOutput,
    DisableUserInput,
    EnableUserInput,
    GetUserInput,
    ListUsersInput,
    ListUsersOutput,
    RemoveUserFromGroupInput,
    RemoveUserFromGroupOutput,
    RevokeSignInSessionsInput,
    RevokeSignInSessionsOutput,
    User,
)

_OBJECT_ID = re.compile(r"[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\Z")
_NEW_UPN_LOCAL = re.compile(r"[A-Za-z0-9'.\-_!#^~]+\Z")
_DOMAIN = re.compile(
    r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+\Z", re.I
)
_ALIAS = re.compile(r"[A-Za-z0-9!#$%&'*+\-/=?^_`{|}~.]+\Z")
_CREATION_KEY = re.compile(r"[A-Za-z0-9._:-]{1,128}\Z")
_USER_SELECT = {"$select": USER_SELECT}


def _validate_user_key(value: str) -> str:
    user_key = value.strip()
    if _OBJECT_ID.fullmatch(user_key):
        return user_key.lower()
    if is_lookup_upn(user_key):
        return user_key
    raise ValueError("userKey must be an object ID or one user principal name such as ada@contoso.com")


def _validate_object_id(name: str, value: str) -> str:
    object_id = value.strip()
    if not _OBJECT_ID.fullmatch(object_id):
        raise ValueError(f"{name} must be an object ID, a GUID such as 00000000-0000-0000-0000-000000000000")
    return object_id.lower()


def _user_path(user_key: str) -> str:
    if user_key.startswith("$"):
        return "/users('" + path_escape(user_key.replace("'", "''")) + "')"
    return "/users/" + path_escape(user_key)


def _receipt(client: Client, call: Call, exchange: Exchange, object_id: str = "") -> Receipt:
    return client.receipt(call, exchange, object_id)


def _failed_query(
    client: Client, call: Call, exchange: Exchange, zero: Any, not_found: str = "notFound"
) -> QueryAttempt[Any]:
    if exchange.outcome == "retry":
        assert exchange.failure is not None
        return QueryAttempt.retry(exchange.failure, exchange.retry_after)
    branch = {"notFound": not_found, "invalidResponse": "invalidResponse", "defect": "defect"}.get(
        exchange.outcome, "providerRejected"
    )
    receipt = Receipt() if exchange.outcome == "defect" else _receipt(client, call, exchange)
    return QueryAttempt.branch_result(branch, zero, receipt, exchange.failure)


def _failed_mutation(
    client: Client, call: Call, exchange: Exchange, zero: Any, not_found: str = "notFound"
) -> MutationAttempt[Any]:
    if exchange.outcome == "retry":
        assert exchange.failure is not None
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    branch = {"notFound": not_found, "invalidResponse": "invalidResponse", "defect": "defect"}.get(
        exchange.outcome, "providerRejected"
    )
    receipt = Receipt() if exchange.outcome == "defect" else _receipt(client, call, exchange)
    return MutationAttempt.branch_result(branch, zero, receipt, exchange.failure)


def _validation_query(operation: str, zero: Any, error: Exception) -> QueryAttempt[Any]:
    return QueryAttempt.branch_result(
        "defect", zero, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
    )


def _validation_mutation(operation: str, zero: Any, error: Exception) -> MutationAttempt[Any]:
    return MutationAttempt.branch_result(
        "defect", zero, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
    )


class GetUserOperation:
    """Read one account by object ID or user principal name."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the generated getUser identity."""
        return definitions.GET_USER_DEFINITION

    async def invoke(self, call: Call, input: GetUserInput) -> QueryAttempt[User]:
        """Select found, a definite read failure, or a safe retry."""
        operation = "getUser"
        try:
            user_key = _validate_user_key(input.user_key)
        except ValueError as error:
            return _validation_query(operation, User(), error)
        try:
            credential = await self.client.resolve_credentials(call)
        except Exception:
            return QueryAttempt.branch_result(
                "defect",
                User(),
                Receipt(),
                failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        exchange = await self.client.exchange(
            call, credential, operation, "GET", _user_path(user_key), _USER_SELECT
        )
        if exchange.outcome != "success":
            return _failed_query(self.client, call, exchange, User())
        receipt = _receipt(self.client, call, exchange)
        try:
            user, _ = decode_user(exchange.body)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                User(),
                receipt,
                failure(
                    operation, FailureKind.PROTOCOL, "provider returned an invalid account: " + str(error)
                ),
            )
        return QueryAttempt.branch_result("found", user, _receipt(self.client, call, exchange, user.id))


class ListUsersOperation:
    """List one bounded Microsoft Graph users page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the generated listUsers identity."""
        return definitions.LIST_USERS_DEFINITION

    async def invoke(self, call: Call, input: ListUsersInput) -> QueryAttempt[ListUsersOutput]:
        """Validate an OData page request, then decode every account."""
        operation = "listUsers"
        try:
            query, next_link, eventual = self._request(input)
        except ValueError as error:
            return _validation_query(operation, ListUsersOutput(), error)
        try:
            credential = await self.client.resolve_credentials(call)
        except Exception:
            return QueryAttempt.branch_result(
                "defect",
                ListUsersOutput(),
                Receipt(),
                failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        exchange = await self.client.exchange(
            call, credential, operation, "GET", "/users", query, eventual=eventual, absolute_url=next_link
        )
        if exchange.outcome != "success":
            return _failed_query(self.client, call, exchange, ListUsersOutput(), "providerRejected")
        receipt = _receipt(self.client, call, exchange)
        try:
            page = json.loads(exchange.body)
            if page is None:
                page = {}
            if not isinstance(page, dict):
                raise ValueError("page is not valid JSON")
            resources = page.get("value") or []
            if not isinstance(resources, list):
                raise ValueError("page is not valid JSON")
            users = []
            for index, resource in enumerate(resources):
                try:
                    users.append(convert_user(resource))
                except ValueError as error:
                    raise ValueError(f"account {index}: {error}") from None
            next_link = page.get("@odata.nextLink") or ""
            if not isinstance(next_link, str):
                raise ValueError("page is not valid JSON")
            if next_link:
                try:
                    self._validate_next_link(next_link)
                except ValueError:
                    raise ValueError("next page link is not a Microsoft Graph users URL") from None
        except (ValueError, TypeError, UnicodeDecodeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                ListUsersOutput(),
                receipt,
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "provider returned an invalid page: page is not valid JSON",
                ),
            )
        return QueryAttempt.branch_result("listed", ListUsersOutput(tuple(users), next_link), receipt)

    def _request(self, input: ListUsersInput) -> tuple[dict[str, str] | None, str, bool]:
        filter_value, search = input.filter.strip(), input.search.strip()
        next_link = input.page_token.strip()
        if next_link:
            if filter_value or search or input.page_size:
                raise ValueError(
                    "leave filter, search, and pageSize blank with pageToken, "
                    "which carries the original query"
                )
            self._validate_next_link(next_link)
            parsed = urlsplit(next_link)
            parameters = parse_qs(parsed.query)
            return (
                None,
                next_link,
                input.uses_advanced_query or "$search" in parameters or "$count" in parameters,
            )
        query = dict(_USER_SELECT)
        if filter_value:
            if len(filter_value.encode()) > 2048 or any(ord(c) < 32 or ord(c) == 127 for c in filter_value):
                raise ValueError("filter must be at most 2048 bytes without control characters")
            query["$filter"] = filter_value
        if search:
            if (
                len(search.encode()) > 1024
                or any(ord(c) < 32 or ord(c) == 127 for c in search)
                or not search.startswith('"')
            ):
                raise ValueError(
                    'search must be a quoted expression such as "displayName:Ada", at most 1024 bytes'
                )
            query["$search"] = search
        if input.uses_advanced_query:
            query["$count"] = "true"
        size = input.page_size or self.client.config.list_users_page_size
        if not 1 <= size <= 999:
            raise ValueError("pageSize must be from 1 to 999")
        query["$top"] = str(size)
        return query, "", input.uses_advanced_query or bool(search)

    @staticmethod
    def _validate_next_link(value: str) -> None:
        parsed = urlsplit(value)
        if (
            len(value) > 8192
            or any(ord(c) < 32 or ord(c) == 127 for c in value)
            or parsed.scheme != "https"
            or parsed.netloc != "graph.microsoft.com"
            or parsed.path != "/v1.0/users"
            or not parsed.query
            or parsed.fragment
        ):
            raise ValueError("pageToken must be the NextPageToken of a previous listUsers page")


class DisableUserOperation:
    """Set accountEnabled false and confirm by reading the account."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return generated disableUser identity."""
        return definitions.DISABLE_USER_DEFINITION

    def idempotency_key(self, call_id: CallID, input: DisableUserInput) -> IdempotencyKey:
        """Repeat an absolute accountEnabled assignment safely."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: DisableUserInput) -> MutationAttempt[User]:
        """Disable and confirm one account."""
        return await _set_account_enabled(self.client, call, input.user_key, False, "disableUser", "disabled")


class EnableUserOperation:
    """Set accountEnabled true and confirm by reading the account."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return generated enableUser identity."""
        return definitions.ENABLE_USER_DEFINITION

    def idempotency_key(self, call_id: CallID, input: EnableUserInput) -> IdempotencyKey:
        """Repeat an absolute accountEnabled assignment safely."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: EnableUserInput) -> MutationAttempt[User]:
        """Enable and confirm one account."""
        return await _set_account_enabled(self.client, call, input.user_key, True, "enableUser", "enabled")


async def _set_account_enabled(
    client: Client, call: Call, raw_key: str, enabled: bool, operation: str, applied: str
) -> MutationAttempt[User]:
    try:
        user_key = _validate_user_key(raw_key)
    except ValueError as error:
        return _validation_mutation(operation, User(), error)
    try:
        credential = await client.resolve_credentials(call)
    except Exception:
        return MutationAttempt.branch_result(
            "defect",
            User(),
            Receipt(),
            failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
        )
    write = await client.exchange(
        call, credential, operation, "PATCH", _user_path(user_key), payload={"accountEnabled": enabled}
    )
    if write.outcome != "success":
        return _failed_mutation(client, call, write, User())
    read = await client.exchange(call, credential, operation, "GET", _user_path(user_key), _USER_SELECT)
    if read.outcome == "notFound" and client.within_propagation_window(call):
        return MutationAttempt.retry(
            failure(
                operation,
                FailureKind.AVAILABILITY,
                "Microsoft Graph accepted the change but does not return the account yet",
            )
        )
    if read.outcome != "success":
        return _failed_mutation(client, call, read, User())
    receipt = _receipt(client, call, read)
    try:
        user, _ = decode_user(read.body)
    except ValueError as error:
        return MutationAttempt.branch_result(
            "invalidResponse",
            User(),
            receipt,
            failure(operation, FailureKind.PROTOCOL, "provider returned an invalid account: " + str(error)),
        )
    receipt = _receipt(client, call, read, user.id)
    if user.is_account_enabled != enabled:
        if client.within_propagation_window(call):
            return MutationAttempt.retry(
                failure(operation, FailureKind.AVAILABILITY, "Microsoft Graph does not report the change yet")
            )
        return MutationAttempt.branch_result(
            "providerRejected",
            user,
            receipt,
            failure(
                operation,
                FailureKind.PROVIDER_REJECTION,
                "Microsoft Graph returned the account without the requested accountEnabled value",
            ),
        )
    return MutationAttempt.branch_result(applied, user, receipt)


class RevokeSignInSessionsOperation:
    """Invalidate one account's Microsoft sign-in sessions."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return generated revokeSignInSessions identity."""
        return definitions.REVOKE_SIGN_IN_SESSIONS_DEFINITION

    def idempotency_key(self, call_id: CallID, input: RevokeSignInSessionsInput) -> IdempotencyKey:
        """Use the Call ID for safely repeatable revocation."""
        return IdempotencyKey(call_id)

    async def invoke(
        self, call: Call, input: RevokeSignInSessionsInput
    ) -> MutationAttempt[RevokeSignInSessionsOutput]:
        """Revoke and classify Microsoft's optional boolean response."""
        operation = "revokeSignInSessions"
        try:
            user_key = _validate_user_key(input.user_key)
        except ValueError as error:
            return _validation_mutation(operation, RevokeSignInSessionsOutput(), error)
        try:
            credential = await self.client.resolve_credentials(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                RevokeSignInSessionsOutput(),
                Receipt(),
                failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        exchange = await self.client.exchange(
            call, credential, operation, "POST", _user_path(user_key) + "/revokeSignInSessions"
        )
        if exchange.outcome != "success":
            return _failed_mutation(self.client, call, exchange, RevokeSignInSessionsOutput())
        receipt = _receipt(self.client, call, exchange, user_key if _OBJECT_ID.fullmatch(user_key) else "")
        output = RevokeSignInSessionsOutput(user_key)
        if not exchange.body.strip():
            return MutationAttempt.branch_result("revoked", output, receipt)
        try:
            response = json.loads(exchange.body)
            if response is None:
                response = {}
            if not isinstance(response, dict) or (
                "value" in response and response["value"] is not None and type(response["value"]) is not bool
            ):
                raise ValueError()
        except (ValueError, UnicodeDecodeError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure(operation, FailureKind.PROTOCOL, "provider returned an invalid revocation response"),
            )
        if response.get("value") is False:
            return MutationAttempt.branch_result(
                "providerRejected",
                output,
                receipt,
                failure(
                    operation,
                    FailureKind.PROVIDER_REJECTION,
                    "Microsoft Graph reported that it did not revoke the sessions",
                ),
            )
        return MutationAttempt.branch_result("revoked", output, receipt)


class AddUserToGroupOperation:
    """Create one direct group membership with read-back convergence."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return generated addUserToGroup identity."""
        return definitions.ADD_USER_TO_GROUP_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddUserToGroupInput) -> IdempotencyKey:
        """Use the Call ID for one convergent membership."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: AddUserToGroupInput) -> MutationAttempt[AddUserToGroupOutput]:
        """Add a member and resolve ambiguous 400 or 404 statuses."""
        operation = "addUserToGroup"
        try:
            group_id = _validate_object_id("groupId", input.group_id)
            user_id = _validate_object_id("userId", input.user_id)
        except ValueError as error:
            return _validation_mutation(operation, AddUserToGroupOutput(), error)
        try:
            credential = await self.client.resolve_credentials(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                AddUserToGroupOutput(),
                Receipt(),
                failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        output = AddUserToGroupOutput(group_id, user_id, False)
        members_path = f"/groups/{group_id}/members"
        exchange = await self.client.exchange(
            call,
            credential,
            operation,
            "POST",
            members_path + "/$ref",
            payload={"@odata.id": GRAPH_BASE + "/directoryObjects/" + user_id},
        )
        if exchange.outcome == "success":
            return MutationAttempt.branch_result(
                "added", output, _receipt(self.client, call, exchange, user_id)
            )
        if exchange.outcome == "badRequest":
            check = await self.client.exchange(
                call,
                credential,
                operation,
                "GET",
                members_path,
                {"$count": "true", "$filter": f"id eq '{user_id}'", "$select": "id"},
                eventual=True,
            )
            if check.outcome != "success":
                return _failed_mutation(self.client, call, check, AddUserToGroupOutput())
            receipt = _receipt(self.client, call, check, user_id)
            try:
                page = json.loads(check.body)
                if page is None:
                    page = {}
                if not isinstance(page, dict) or not isinstance(page.get("value", []), list):
                    raise ValueError()
                members = page.get("value") or []
                if any(
                    member is not None
                    and (
                        not isinstance(member, dict)
                        or (member.get("id") is not None and not isinstance(member.get("id"), str))
                    )
                    for member in members
                ):
                    raise ValueError()
            except (ValueError, UnicodeDecodeError):
                return MutationAttempt.branch_result(
                    "invalidResponse",
                    AddUserToGroupOutput(),
                    receipt,
                    failure(operation, FailureKind.PROTOCOL, "provider returned an invalid membership check"),
                )
            if any((member or {}).get("id", "").lower() == user_id for member in members):
                return MutationAttempt.branch_result(
                    "added", replace(output, was_already_member=True), receipt
                )
            if self.client.within_propagation_window(call):
                return MutationAttempt.retry(
                    failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "Microsoft Graph rejected the membership but does not list it yet",
                    )
                )
            return MutationAttempt.branch_result(
                "providerRejected",
                AddUserToGroupOutput(),
                _receipt(self.client, call, exchange),
                exchange.failure,
            )
        if exchange.outcome == "notFound":
            group = await self.client.exchange(
                call, credential, operation, "GET", f"/groups/{group_id}", {"$select": "id"}
            )
            if group.outcome != "success":
                return _failed_mutation(self.client, call, group, AddUserToGroupOutput())
            if self.client.within_propagation_window(call):
                return MutationAttempt.retry(
                    failure(
                        operation, FailureKind.AVAILABILITY, "Microsoft Graph does not return the account yet"
                    )
                )
            return MutationAttempt.branch_result(
                "notFound",
                AddUserToGroupOutput(),
                _receipt(self.client, call, group),
                failure(operation, FailureKind.NOT_FOUND, "account was not found"),
            )
        return _failed_mutation(self.client, call, exchange, AddUserToGroupOutput())


class RemoveUserFromGroupOperation:
    """Delete a direct membership reference without deleting the account."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return generated removeUserFromGroup identity."""
        return definitions.REMOVE_USER_FROM_GROUP_DEFINITION

    def idempotency_key(self, call_id: CallID, input: RemoveUserFromGroupInput) -> IdempotencyKey:
        """Use the Call ID for one convergent removal."""
        return IdempotencyKey(call_id)

    async def invoke(
        self, call: Call, input: RemoveUserFromGroupInput
    ) -> MutationAttempt[RemoveUserFromGroupOutput]:
        """Remove the member and confirm a 404 group still exists."""
        operation = "removeUserFromGroup"
        try:
            group_id = _validate_object_id("groupId", input.group_id)
            user_id = _validate_object_id("userId", input.user_id)
        except ValueError as error:
            return _validation_mutation(operation, RemoveUserFromGroupOutput(), error)
        try:
            credential = await self.client.resolve_credentials(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                RemoveUserFromGroupOutput(),
                Receipt(),
                failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        output = RemoveUserFromGroupOutput(group_id, user_id, False)
        exchange = await self.client.exchange(
            call, credential, operation, "DELETE", f"/groups/{group_id}/members/{user_id}/$ref"
        )
        if exchange.outcome == "success":
            return MutationAttempt.branch_result(
                "removed", output, _receipt(self.client, call, exchange, user_id)
            )
        if exchange.outcome == "notFound":
            group = await self.client.exchange(
                call, credential, operation, "GET", f"/groups/{group_id}", {"$select": "id"}
            )
            if group.outcome != "success":
                return _failed_mutation(self.client, call, group, RemoveUserFromGroupOutput())
            return MutationAttempt.branch_result(
                "removed",
                replace(output, was_already_removed=True),
                _receipt(self.client, call, group, user_id),
            )
        return _failed_mutation(self.client, call, exchange, RemoveUserFromGroupOutput())


class CreateUserOperation:
    """Create one account and converge on its extension-attribute key."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return generated createUser identity."""
        return definitions.CREATE_USER_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateUserInput) -> IdempotencyKey:
        """Use a stable provisioning key when supplied, or the Call ID."""
        return IdempotencyKey(input.provisioning_key.strip() or call_id)

    async def invoke(self, call: Call, input: CreateUserInput) -> MutationAttempt[CreateUserOutput]:
        """Create, read back conflicts, and retry unusable create answers."""
        operation = "createUser"
        key = str(call.idempotency_key)
        try:
            payload = _build_create_user_request(input, self.client.config.creation_key_attribute, key)
        except ValueError as error:
            return _validation_mutation(operation, CreateUserOutput(), error)
        try:
            credential = await self.client.resolve_credentials(call)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                CreateUserOutput(),
                Receipt(),
                failure(operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"),
            )
        try:
            payload["passwordProfile"]["password"] = _initial_password(self.client.random_source)
        except Exception:
            return MutationAttempt.branch_result(
                "defect",
                CreateUserOutput(),
                Receipt(),
                failure(operation, FailureKind.LOCAL_DEFECT, "initial password could not be generated"),
            )
        exchange = await self.client.exchange(call, credential, operation, "POST", "/users", payload=payload)
        if exchange.outcome == "success":
            try:
                user, _ = decode_user(exchange.body, False)
                if user.user_principal_name.lower() != payload["userPrincipalName"].lower():
                    raise ValueError()
            except ValueError:
                return MutationAttempt.retry(
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "create response is unusable; the retry reads the account back",
                    )
                )
            user = replace(
                user,
                is_account_enabled=True,
                mail_nickname=user.mail_nickname or payload["mailNickname"],
                department=user.department or payload.get("department", ""),
                usage_location=user.usage_location or payload.get("usageLocation", ""),
            )
            return MutationAttempt.branch_result(
                "created", CreateUserOutput(user, False), _receipt(self.client, call, exchange, user.id)
            )
        if exchange.outcome == "badRequest":
            read = await self.client.exchange(
                call,
                credential,
                operation,
                "GET",
                _user_path(payload["userPrincipalName"]),
                {"$select": CREATION_SELECT},
            )
            if read.outcome == "success":
                receipt = _receipt(self.client, call, read)
                try:
                    user, source = decode_user(read.body)
                except ValueError as error:
                    return MutationAttempt.branch_result(
                        "invalidResponse",
                        CreateUserOutput(),
                        receipt,
                        failure(
                            operation,
                            FailureKind.PROTOCOL,
                            "provider returned an invalid account: " + str(error),
                        ),
                    )
                receipt = _receipt(self.client, call, read, user.id)
                attributes = source.get("onPremisesExtensionAttributes") or {}
                if not isinstance(attributes, dict) or any(
                    value is not None and not isinstance(value, str) for value in attributes.values()
                ):
                    return MutationAttempt.branch_result(
                        "invalidResponse",
                        CreateUserOutput(),
                        receipt,
                        failure(
                            operation,
                            FailureKind.PROTOCOL,
                            "provider returned an invalid account: account is not valid JSON",
                        ),
                    )
                if attributes.get(self.client.config.creation_key_attribute) == "dex-creation-key:" + key:
                    return MutationAttempt.branch_result("created", CreateUserOutput(user, True), receipt)
                return MutationAttempt.branch_result(
                    "alreadyExists",
                    CreateUserOutput(user, False),
                    receipt,
                    failure(
                        operation,
                        FailureKind.CONFLICT,
                        "user principal name belongs to an account this Step did not create",
                    ),
                )
            if read.outcome == "notFound":
                if self.client.within_propagation_window(call):
                    return MutationAttempt.retry(
                        failure(
                            operation,
                            FailureKind.AVAILABILITY,
                            "Microsoft Graph rejected the account but does not return "
                            "the user principal name yet",
                        )
                    )
                try:
                    document = json.loads(exchange.body)
                    details = document.get("error", {}).get("details", [])
                    has_conflict = any(
                        isinstance(item, dict) and item.get("code") == "ObjectConflict"
                        for item in details[:3]
                    )
                except (ValueError, TypeError, AttributeError):
                    has_conflict = False
                if has_conflict:
                    return MutationAttempt.branch_result(
                        "alreadyExists",
                        CreateUserOutput(),
                        _receipt(self.client, call, read),
                        failure(
                            operation,
                            FailureKind.CONFLICT,
                            "user principal name or mail alias belongs to an object outside "
                            "the user directory, such as a deleted account",
                        ),
                    )
                return MutationAttempt.branch_result(
                    "providerRejected",
                    CreateUserOutput(),
                    _receipt(self.client, call, exchange),
                    exchange.failure,
                )
            return _failed_mutation(self.client, call, read, CreateUserOutput(), "providerRejected")
        if exchange.outcome == "invalidResponse":
            assert exchange.failure is not None
            return MutationAttempt.retry(
                failure(
                    operation,
                    exchange.failure.kind,
                    "create response is unusable; the retry reads the account back",
                )
            )
        if exchange.outcome in ("notFound", "rejected"):
            return MutationAttempt.branch_result(
                "providerRejected",
                CreateUserOutput(),
                _receipt(self.client, call, exchange),
                exchange.failure,
            )
        return _failed_mutation(self.client, call, exchange, CreateUserOutput(), "providerRejected")


def _build_create_user_request(input: CreateUserInput, attribute: str, key: str) -> dict[str, Any]:
    principal = input.user_principal_name.strip()
    parts = principal.split("@")
    invalid = (
        "userPrincipalName must be alias@domain with at most 64 letters, digits, or ' . - _ ! # ^ ~ "
        "before the @ and a verified domain after it"
    )
    if (
        len(parts) != 2
        or len(principal.encode()) > 113
        or len(parts[0].encode()) > 64
        or len(parts[1].encode()) > 48
        or not _NEW_UPN_LOCAL.fullmatch(parts[0])
        or parts[0].startswith(".")
        or parts[0].endswith(".")
        or not _DOMAIN.fullmatch(parts[1])
    ):
        raise ValueError(invalid)
    if not _CREATION_KEY.fullmatch(key):
        raise ValueError("provisioningKey must be 1 to 128 letters, digits, or . _ : - characters")

    def text(name: str, value: str, length: int) -> str:
        value = value.strip()
        if len(value) > length:
            raise ValueError(f"{name} must be valid text of at most {length} characters")
        if any(unicodedata.category(c) == "Cc" for c in value):
            raise ValueError(f"{name} cannot contain control characters")
        return value

    given = text("givenName", input.given_name, 64)
    surname = text("surname", input.surname, 64)
    display = text("displayName", input.display_name, 256) or (given + " " + surname).strip()
    if not display:
        raise ValueError("displayName, givenName, or surname is required")
    title = text("jobTitle", input.job_title, 128)
    department = text("department", input.department, 64)
    alias = input.mail_nickname.strip() or parts[0]
    if len(alias.encode()) > 64 or not _ALIAS.fullmatch(alias):
        raise ValueError(
            "mailNickname must be at most 64 letters, digits, or alias punctuation without spaces or @"
        )
    location = input.usage_location.strip()
    if location and not re.fullmatch(r"[A-Z]{2}", location):
        raise ValueError("usageLocation must be a two-letter uppercase ISO 3166 country code such as US")
    request: dict[str, Any] = {
        "accountEnabled": True,
        "displayName": display,
        "mailNickname": alias,
        "userPrincipalName": principal,
        "passwordProfile": {"forceChangePasswordNextSignIn": True},
        "onPremisesExtensionAttributes": {attribute: "dex-creation-key:" + key},
    }
    for name, value in (
        ("givenName", given),
        ("surname", surname),
        ("jobTitle", title),
        ("department", department),
        ("usageLocation", location),
    ):
        if value:
            request[name] = value
    return request


def _initial_password(source: Any) -> str:
    classes = ("abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0123456789", "!#%*+-=?@^_~")
    alphabet = "".join(classes)
    choose = source.go_random_int if source is not None else secrets.randbelow
    password = [characters[choose(len(characters))] for characters in classes]
    password.extend(alphabet[choose(len(alphabet))] for _ in range(40))
    for index in range(len(password) - 1, 0, -1):
        swap = choose(index + 1)
        password[index], password[swap] = password[swap], password[index]
    return "".join(password)
