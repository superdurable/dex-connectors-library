# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Contacts provider decisions matching Go conformance recordings."""

from __future__ import annotations

import json
import re
from datetime import timedelta
from typing import Any, TypeVar
from urllib.parse import urlencode

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
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated import definitions
from .client import Client, Exchange
from .models import Contact, ContactsPage, CreateContactInput, GetContactInput, ListContactsInput

Output = TypeVar("Output")
_RESOURCE_NAME = re.compile(r"people/[A-Za-z0-9_-]{1,128}\Z")


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "google-contacts", operation, message)


def _branch(
    operation: str, branch: str, kind: FailureKind, message: str, empty: Output
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(branch, empty, Receipt(), _failure(operation, kind, message))


def _retry(operation: str, kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[Any]:
    return QueryAttempt.retry(_failure(operation, kind, message), delay)


def _read_failure(operation: str, exchange: Exchange, empty: Output) -> QueryAttempt[Output] | None:
    if exchange.error == "credential":
        return _branch(
            operation,
            "defect",
            FailureKind.AUTHENTICATION,
            "connection credentials are unavailable or invalid",
            empty,
        )
    if exchange.error in ("request", "refresh", "not_built"):
        return _retry(operation, FailureKind.TRANSPORT, "Google Contacts request failed")
    if exchange.status == 429:
        return _retry(
            operation, FailureKind.RATE_LIMIT, "Google rate limited the request", exchange.retry_after
        )
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(
            operation,
            FailureKind.AVAILABILITY,
            "Google Contacts is temporarily unavailable",
            exchange.retry_after,
        )
    if exchange.status != 200:
        return _branch(
            operation,
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Google rejected HTTP {exchange.status}",
            empty,
        )
    if exchange.error == "too_large":
        return _branch(
            operation,
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Google response exceeded the configured limit",
            empty,
        )
    if exchange.error:
        return _retry(operation, FailureKind.TRANSPORT, "Google response was interrupted")
    return None


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _valid_token(value: str) -> bool:
    return isinstance(value, str) and len(value.encode()) <= 4096 and "\r" not in value and "\n" not in value


def _contact(person: Any) -> Contact | None:
    if not isinstance(person, dict) or not isinstance(person.get("resourceName"), str):
        return None
    resource_name = person["resourceName"]
    if not _RESOURCE_NAME.fullmatch(resource_name):
        return None
    names = person.get("names")
    emails = person.get("emailAddresses")
    if names is None:
        names = []
    if emails is None:
        emails = []
    if not isinstance(names, list) or not isinstance(emails, list):
        return None
    if any(not isinstance(item, dict) or not isinstance(item.get("displayName", ""), str) for item in names):
        return None
    if any(not isinstance(item, dict) or not isinstance(item.get("value", ""), str) for item in emails):
        return None
    return Contact(
        resource_name,
        names[0].get("displayName", "") if names else "",
        emails[0].get("value", "") if emails else "",
    )


class ListContactsOperation:
    """Read one page of contact identities."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated contact page Query contract."""
        return definitions.LIST_CONTACTS_DEFINITION

    async def invoke(self, call: Call, input: ListContactsInput) -> QueryAttempt[ContactsPage]:
        """List contacts with a fixed names and email field mask."""
        empty = ContactsPage()
        if input.page_size < 0 or input.page_size > 1000 or not _valid_token(input.page_token):
            return _branch(
                "listContacts", "defect", FailureKind.VALIDATION, "contact page input is invalid", empty
            )
        query = {"personFields": "names,emailAddresses", "pageSize": str(input.page_size or 100)}
        if input.page_token:
            query["pageToken"] = input.page_token
        path = "/v1/people/me/connections?" + urlencode(sorted(query.items()))
        exchange = await self.client.request(call, path)
        failure = _read_failure("listContacts", exchange, empty)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch(
                "listContacts",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned invalid contact page JSON",
                empty,
            )
        entries = document.get("connections", [])
        next_token = document.get("nextPageToken", "")
        if (
            not isinstance(entries, list)
            or len(entries) > (input.page_size or 100)
            or not _valid_token(next_token)
        ):
            return _branch(
                "listContacts",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned invalid contact page JSON",
                empty,
            )
        contacts = [_contact(person) for person in entries]
        if any(contact is None for contact in contacts):
            return _branch(
                "listContacts",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned an invalid contact resource",
                empty,
            )
        return QueryAttempt.branch_result(
            "listed",
            ContactsPage(tuple(contact for contact in contacts if contact is not None), next_token),
            Receipt(),
        )


class GetContactOperation:
    """Read one contact by People API resource name."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated contact Query contract."""
        return definitions.GET_CONTACT_DEFINITION

    async def invoke(self, call: Call, input: GetContactInput) -> QueryAttempt[Contact]:
        """Get a contact without retaining other People API fields."""
        empty = Contact()
        if not _RESOURCE_NAME.fullmatch(input.resource_name):
            return _branch(
                "getContact", "defect", FailureKind.VALIDATION, "contact resource name is invalid", empty
            )
        path = "/v1/" + input.resource_name + "?" + urlencode({"personFields": "names,emailAddresses"})
        exchange = await self.client.request(call, path)
        if exchange.status == 404 and not exchange.error:
            return _branch(
                "getContact", "notFound", FailureKind.PROVIDER_REJECTION, "contact was not found", empty
            )
        failure = _read_failure("getContact", exchange, empty)
        if failure is not None:
            return failure
        contact = _contact(_document(exchange.body))
        if contact is None or contact.resource_name != input.resource_name:
            return _branch(
                "getContact",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned the wrong contact resource",
                empty,
            )
        return QueryAttempt.branch_result("found", contact, Receipt())


class CreateContactOperation:
    """Create one contact with a durable single-dispatch fence."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated contact-creation Mutation contract."""
        return definitions.CREATE_CONTACT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateContactInput) -> IdempotencyKey:
        """Record Call identity; Google does not accept it as a deduplication key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateContactInput) -> MutationAttempt[Contact]:
        """Send at most once unless Google definitively confirms a safe retry."""
        try:
            fence = Fence(call, timedelta(seconds=30))
            decision = await fence.decide()
        except ValueError:
            return self._failure(
                "defect", FailureKind.LOCAL_DEFECT, "Google Contacts dispatch fence is unavailable"
            )
        if decision.kind != DecisionKind.SEND_ALLOWED:
            return self._uncertain(
                FailureKind.TRANSPORT,
                "contact creation outcome cannot be confirmed; inspect Contacts before creating another",
            )
        if not self._valid_input(input):
            return self._failure("defect", FailureKind.VALIDATION, "contact name or email is invalid")
        body: dict[str, Any] = {"names": [{"givenName": input.given_name}]}
        if input.family_name:
            body["names"][0]["familyName"] = input.family_name
        if input.email_address:
            body["emailAddresses"] = [{"value": input.email_address}]
        payload = json.dumps(body, separators=(",", ":"), ensure_ascii=False).encode()
        token, error = await self.client.resolve_token(call)
        if error == "credential":
            return self._failure(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        if error:
            try:
                await fence.record_not_sent()
            except Exception:
                return self._retry(FailureKind.TRANSPORT, "credential refresh checkpoint is unavailable")
            return self._retry(FailureKind.TRANSPORT, "connection credentials could not be refreshed")
        path = "/v1/people:createContact?" + urlencode({"personFields": "names,emailAddresses"})
        try:
            exchange = await fence.dispatch(
                lambda _: self.client.request(call, path, token=token, method="POST", body=payload)
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return self._retry(FailureKind.TRANSPORT, "contact dispatch was not started")
        except Exception:
            return self._uncertain(FailureKind.TRANSPORT, "contact creation outcome is unknown")
        if exchange.status in (429, 503):
            try:
                await fence.record_can_resend()
            except Exception:
                return self._retry(FailureKind.AVAILABILITY, "Google retry checkpoint is unavailable")
            kind = FailureKind.RATE_LIMIT if exchange.status == 429 else FailureKind.AVAILABILITY
            return self._retry(kind, "Google did not create the contact", exchange.retry_after)
        if exchange.status >= 500 or exchange.status in (408, 409):
            return self._uncertain(FailureKind.AVAILABILITY, "contact creation outcome is unknown")
        if exchange.error:
            if exchange.error == "not_built" or (exchange.error == "request" and not exchange.request_sent):
                try:
                    await fence.record_can_resend()
                except Exception:
                    return self._uncertain(FailureKind.TRANSPORT, "contact creation outcome is unknown")
                return self._retry(FailureKind.TRANSPORT, "Google could not be reached before sending")
            return self._uncertain(FailureKind.TRANSPORT, "contact creation outcome is unknown")
        if exchange.status == 401:
            return self._failure(
                "providerRejected", FailureKind.AUTHENTICATION, "Google rejected contact authorization"
            )
        if exchange.status == 403:
            return self._failure(
                "providerRejected", FailureKind.AUTHORIZATION, "Google denied contact creation"
            )
        if 400 <= exchange.status < 500:
            return self._failure(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Google rejected HTTP {exchange.status}"
            )
        if exchange.status not in (200, 201):
            return self._uncertain(FailureKind.PROTOCOL, "Google returned an unexpected contact response")
        contact = _contact(_document(exchange.body))
        if contact is None:
            return self._uncertain(
                FailureKind.PROTOCOL, "Google created a contact but returned no usable resource name"
            )
        try:
            await fence.record_provider_id(contact.resource_name)
        except Exception:
            pass  # A lost provider-ID heartbeat still forbids another send.
        return MutationAttempt.branch_result("created", contact, Receipt())

    @staticmethod
    def _valid_input(input: CreateContactInput) -> bool:
        values = (input.given_name, input.family_name, input.email_address)
        try:
            lengths = tuple(len(value.encode()) for value in values)
        except UnicodeEncodeError:
            return False
        if not input.given_name.strip() or lengths[0] > 256 or lengths[1] > 256 or lengths[2] > 320:
            return False
        email = input.email_address
        return not (
            "\r" in email or "\n" in email or (email and ("@" not in email or " " in email or "\t" in email))
        )

    @staticmethod
    def _failure(branch: str, kind: FailureKind, message: str) -> MutationAttempt[Contact]:
        return MutationAttempt.branch_result(
            branch, Contact(), Receipt(), _failure("createContact", kind, message)
        )

    @staticmethod
    def _retry(kind: FailureKind, message: str, delay: float = 0) -> MutationAttempt[Contact]:
        return MutationAttempt.retry(_failure("createContact", kind, message), delay)

    @staticmethod
    def _uncertain(kind: FailureKind, message: str) -> MutationAttempt[Contact]:
        return MutationAttempt.uncertain(Contact(), _failure("createContact", kind, message), Receipt())
