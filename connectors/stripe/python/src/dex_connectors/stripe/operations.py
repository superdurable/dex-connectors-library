# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Stripe operation definitions, input checks, and attempt classification."""

import json
import re
from datetime import timedelta
from email.utils import parseaddr
from typing import cast
from urllib.parse import urlsplit

from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    StepDefaults,
)
from dex_connectors.sdk.providerhttp import ProviderTransportError

from .client import (
    Client,
    ProviderResponse,
    classify_stripe_rejection,
    decode_checkout_session,
    decode_payment_intent,
    decode_refund,
    parse_stripe_api_error,
    retry_after_seconds,
    should_retry_keyed_conflict,
    should_retry_keyed_server_failure,
    stripe_failure,
)
from .models import (
    CheckoutSession,
    CreateACHCheckoutSessionInput,
    CreateCardCheckoutSessionInput,
    CreateRefundInput,
    GetCheckoutSessionInput,
    GetPaymentIntentInput,
    PaymentIntent,
    Refund,
)

_DEFAULTS = StepDefaults(timedelta(seconds=30), 2)


def _definition(operation: str, happy_path: str, is_mutation: bool) -> QueryDefinition | MutationDefinition:
    branches = [BranchDefinition(happy_path, f"Stripe returned {happy_path}.")]
    if not is_mutation:
        branches.append(BranchDefinition("notFound", "Stripe did not find the resource.", True))
    branches.append(BranchDefinition("providerRejected", "Stripe conclusively rejected the request.", True))
    if is_mutation:
        branches.append(BranchDefinition("uncertain", "Stripe could not confirm the write outcome.", True))
    branches.extend(
        (
            BranchDefinition("invalidResponse", "Stripe returned an invalid or oversized response.", True),
            BranchDefinition("defect", "Local input, connection, or connector definition is invalid.", True),
        )
    )
    definition_type = MutationDefinition if is_mutation else QueryDefinition
    return definition_type(OperationRef("stripe", operation), tuple(branches), _DEFAULTS)


CREATE_CARD_CHECKOUT_SESSION_DEFINITION = cast(
    MutationDefinition, _definition("createCardCheckoutSession", "created", True)
)
CREATE_ACH_CHECKOUT_SESSION_DEFINITION = cast(
    MutationDefinition, _definition("createACHCheckoutSession", "created", True)
)
GET_CHECKOUT_SESSION_DEFINITION = _definition("getCheckoutSession", "found", False)
GET_PAYMENT_INTENT_DEFINITION = _definition("getPaymentIntent", "found", False)
CREATE_REFUND_DEFINITION = cast(MutationDefinition, _definition("createRefund", "refunded", True))


def _validate_payment_intent_id(identifier: str) -> None:
    if not identifier.startswith("pi_") or any(character in identifier for character in "/?#"):
        raise ValueError("Stripe PaymentIntent ID must be a pi_ identifier")


def _validate_metadata(metadata: dict[str, str]) -> None:
    if len(metadata) > 50:
        raise ValueError("metadata cannot contain more than 50 fields")
    for key, value in metadata.items():
        if not key.strip() or len(key) > 40 or "[" in key or "]" in key:
            raise ValueError(
                "metadata keys must be non-empty, at most 40 characters, and contain no square brackets"
            )
        if len(value) > 500:
            raise ValueError("metadata values cannot exceed 500 characters")


def _validate_redirect_url(value: str, field: str) -> None:
    parsed = urlsplit(value)
    if not parsed.scheme or not parsed.hostname or parsed.username or parsed.password:
        raise ValueError(f"{field} URL must be absolute and must not contain credentials")
    if parsed.scheme != "https" and not (
        parsed.scheme == "http" and parsed.hostname in ("localhost", "127.0.0.1")
    ):
        raise ValueError(f"{field} URL must use HTTPS")


def _checkout_form(
    input_value: CreateACHCheckoutSessionInput | CreateCardCheckoutSessionInput, payment_method: str
) -> dict[str, str]:
    if not input_value.client_reference_id.strip() or len(input_value.client_reference_id) > 200:
        raise ValueError("client reference ID must be non-empty and at most 200 characters")
    display_name, address = parseaddr(input_value.customer_email.strip())
    if display_name or address != input_value.customer_email.strip() or "@" not in address:
        raise ValueError("customer email must be one plain email address")
    _validate_redirect_url(input_value.success_url, "success")
    _validate_redirect_url(input_value.cancel_url, "cancel")
    if not re.fullmatch(r"[a-z]{3}", input_value.currency):
        raise ValueError("currency must be a lowercase three-letter code")
    if input_value.unit_amount <= 0 or input_value.unit_amount > 99_999_999:
        raise ValueError("unit amount must be from 1 through 99999999")
    if not input_value.product_name.strip() or len(input_value.product_name) > 250:
        raise ValueError("product name must be non-empty and at most 250 characters")
    _validate_metadata(input_value.metadata)
    values = {
        "mode": "payment",
        "payment_method_types[0]": payment_method,
        "client_reference_id": input_value.client_reference_id,
        "customer_email": input_value.customer_email,
        "success_url": input_value.success_url,
        "cancel_url": input_value.cancel_url,
        "line_items[0][quantity]": "1",
        "line_items[0][price_data][currency]": input_value.currency,
        "line_items[0][price_data][unit_amount]": str(input_value.unit_amount),
        "line_items[0][price_data][product_data][name]": input_value.product_name,
    }
    for key, value in input_value.metadata.items():
        values[f"metadata[{key}]"] = value
        values[f"payment_intent_data[metadata][{key}]"] = value
    if input_value.expires_at is not None:
        values["expires_at"] = str(int(input_value.expires_at.timestamp()))
    return values


def _refund_form(input_value: CreateRefundInput, call: Call) -> dict[str, str]:
    _validate_payment_intent_id(input_value.payment_intent_id)
    if input_value.amount <= 0:
        raise ValueError("refund amount must be positive")
    if input_value.reason not in ("", "duplicate", "fraudulent", "requested_by_customer"):
        raise ValueError("refund reason must be duplicate, fraudulent, or requested_by_customer")
    if "dex_call_id" in input_value.metadata:
        raise ValueError('refund metadata key "dex_call_id" is reserved for call reconciliation')
    if len(input_value.metadata) >= 50:
        raise ValueError("refund metadata cannot contain more than 49 application fields")
    _validate_metadata(input_value.metadata)
    values = {"payment_intent": input_value.payment_intent_id, "amount": str(input_value.amount)}
    if input_value.reason:
        values["reason"] = input_value.reason
    for key, value in input_value.metadata.items():
        values[f"metadata[{key}]"] = value
    values["metadata[dex_call_id]"] = str(call.id)
    return values


def _refund_rejection(response: ProviderResponse) -> Failure:
    classified = classify_stripe_rejection("createRefund", response)
    summary = "Stripe rejected the refund"
    if response.status_code not in (401, 403):
        api_error = parse_stripe_api_error(response.body)
        if api_error.get("code"):
            summary += ": " + api_error["code"]
        if api_error.get("message"):
            summary += ": " + api_error["message"][:300]
    return stripe_failure("createRefund", classified.kind, summary)


class _CheckoutMutation:
    """Common keyed Checkout classification for card and ACH operations."""

    def __init__(self, client: Client, operation: str, payment_method: str) -> None:
        self._client = client
        self._operation = operation
        self._payment_method = payment_method

    def idempotency_key(
        self, call_id: CallID, input: CreateACHCheckoutSessionInput | CreateCardCheckoutSessionInput
    ) -> IdempotencyKey:
        """Use the stable connector call ID as Stripe's idempotency key."""
        return IdempotencyKey(call_id)

    async def invoke(
        self, call: Call, input: CreateACHCheckoutSessionInput | CreateCardCheckoutSessionInput
    ) -> MutationAttempt[CheckoutSession]:
        """Send one keyed Stripe Checkout request and classify its outcome."""
        empty = CheckoutSession()
        try:
            values = _checkout_form(input, self._payment_method)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                self._client.receipt(call),
                stripe_failure(self._operation, FailureKind.VALIDATION, str(error)),
            )
        credentials, failure = await self._client.resolve_credentials(call, self._operation)
        if failure is not None or credentials is None:
            return MutationAttempt.branch_result("defect", empty, self._client.receipt(call), failure)
        try:
            response = await self._client.request(
                call, credentials.secret_key.reveal(), "POST", "checkout/sessions", values
            )
        except ProviderTransportError:
            return MutationAttempt.retry(
                stripe_failure(
                    self._operation, FailureKind.TRANSPORT, "Stripe Checkout Session response is unavailable"
                )
            )
        except ValueError:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                self._client.receipt(call),
                stripe_failure(self._operation, FailureKind.LOCAL_DEFECT, "Stripe request is invalid"),
            )
        if response.status_code == 429:
            return MutationAttempt.retry(
                stripe_failure(
                    self._operation,
                    FailureKind.RATE_LIMIT,
                    "Stripe rate limited the Checkout Session request",
                ),
                retry_after_seconds(response.headers),
            )
        if response.status_code == 409 and should_retry_keyed_conflict(response):
            return MutationAttempt.retry(
                stripe_failure(
                    self._operation,
                    FailureKind.CONFLICT,
                    "Stripe reported a conflicting Checkout Session request with this idempotency key",
                ),
                retry_after_seconds(response.headers),
            )
        if response.status_code >= 500:
            if should_retry_keyed_server_failure(response):
                return MutationAttempt.retry(
                    stripe_failure(
                        self._operation,
                        FailureKind.AVAILABILITY,
                        "Stripe Checkout Session is temporarily unavailable",
                    ),
                    retry_after_seconds(response.headers),
                )
            return MutationAttempt(
                "uncertain",
                "uncertain",
                empty,
                self._client.receipt(call, response),
                stripe_failure(
                    self._operation,
                    FailureKind.AVAILABILITY,
                    "Stripe Checkout Session outcome cannot be confirmed after a server error",
                ),
            )
        if not 200 <= response.status_code < 300:
            return MutationAttempt.branch_result(
                "providerRejected",
                empty,
                self._client.receipt(call, response),
                classify_stripe_rejection(self._operation, response),
            )
        if response.is_body_too_large:
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                self._client.receipt(call, response),
                stripe_failure(
                    self._operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Stripe response exceeds configured size limit",
                ),
            )
        try:
            session = decode_checkout_session(response.body)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                self._client.receipt(call, response),
                stripe_failure(self._operation, FailureKind.PROTOCOL, str(error)),
            )
        return MutationAttempt.branch_result(
            "created", session, self._client.receipt(call, response, session.id)
        )


class CreateCardCheckoutSessionOperation(_CheckoutMutation):
    """Create one hosted card Checkout Session with Stripe deduplication."""

    def __init__(self, client: Client) -> None:
        super().__init__(client, "createCardCheckoutSession", "card")

    def definition(self) -> MutationDefinition:
        """Return the card Checkout definition and stable branch identities."""
        return CREATE_CARD_CHECKOUT_SESSION_DEFINITION


class CreateACHCheckoutSessionOperation(_CheckoutMutation):
    """Create one hosted US bank account Checkout Session with deduplication."""

    def __init__(self, client: Client) -> None:
        super().__init__(client, "createACHCheckoutSession", "us_bank_account")

    def definition(self) -> MutationDefinition:
        """Return the ACH Checkout definition and stable branch identities."""
        return CREATE_ACH_CHECKOUT_SESSION_DEFINITION


class GetCheckoutSessionOperation:
    """Read one Stripe Checkout Session without exposing payment details."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> QueryDefinition:
        """Return the Checkout query definition and stable branches."""
        return GET_CHECKOUT_SESSION_DEFINITION

    async def invoke(self, call: Call, input: GetCheckoutSessionInput) -> QueryAttempt[CheckoutSession]:
        """Read and classify one Checkout Session query attempt."""
        operation = "getCheckoutSession"
        empty = CheckoutSession()
        session_id = input.session_id.strip()
        if not session_id.startswith("cs_") or any(character in session_id for character in "/?#"):
            return QueryAttempt.branch_result(
                "defect",
                empty,
                self._client.receipt(call),
                stripe_failure(operation, FailureKind.VALIDATION, "Stripe Checkout Session ID is required"),
            )
        credentials, failure = await self._client.resolve_credentials(call, operation)
        if failure is not None or credentials is None:
            return QueryAttempt.branch_result("defect", empty, self._client.receipt(call), failure)
        try:
            response = await self._client.request(
                call, credentials.secret_key.reveal(), "GET", f"checkout/sessions/{session_id}"
            )
        except ProviderTransportError:
            return QueryAttempt.retry(
                stripe_failure(
                    operation, FailureKind.AVAILABILITY, "Stripe Checkout Session is temporarily unavailable"
                )
            )
        if response.status_code == 429:
            return QueryAttempt.retry(
                stripe_failure(
                    operation, FailureKind.RATE_LIMIT, "Stripe rate limited the Checkout Session query"
                ),
                retry_after_seconds(response.headers),
            )
        if response.status_code >= 500:
            return QueryAttempt.retry(
                stripe_failure(
                    operation, FailureKind.AVAILABILITY, "Stripe Checkout Session is temporarily unavailable"
                )
            )
        if response.status_code == 404:
            return QueryAttempt.branch_result(
                "notFound",
                empty,
                self._client.receipt(call, response, session_id),
                stripe_failure(operation, FailureKind.NOT_FOUND, "Stripe Checkout Session was not found"),
            )
        if not 200 <= response.status_code < 300:
            return QueryAttempt.branch_result(
                "providerRejected",
                empty,
                self._client.receipt(call, response, session_id),
                classify_stripe_rejection(operation, response),
            )
        if response.is_body_too_large:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self._client.receipt(call, response, session_id),
                stripe_failure(
                    operation, FailureKind.RESPONSE_TOO_LARGE, "Stripe response exceeds configured size limit"
                ),
            )
        try:
            session = decode_checkout_session(response.body)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self._client.receipt(call, response, session_id),
                stripe_failure(operation, FailureKind.PROTOCOL, str(error)),
            )
        return QueryAttempt.branch_result("found", session, self._client.receipt(call, response, session.id))


class GetPaymentIntentOperation:
    """Read one PaymentIntent and its latest charge's refunded amount."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> QueryDefinition:
        """Return the PaymentIntent query definition and stable branches."""
        return GET_PAYMENT_INTENT_DEFINITION

    async def invoke(self, call: Call, input: GetPaymentIntentInput) -> QueryAttempt[PaymentIntent]:
        """Read and classify one PaymentIntent query attempt."""
        operation = "getPaymentIntent"
        empty = PaymentIntent()
        payment_intent_id = input.payment_intent_id.strip()
        try:
            _validate_payment_intent_id(payment_intent_id)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                self._client.receipt(call),
                stripe_failure(operation, FailureKind.VALIDATION, str(error)),
            )
        credentials, failure = await self._client.resolve_credentials(call, operation)
        if failure is not None or credentials is None:
            return QueryAttempt.branch_result("defect", empty, self._client.receipt(call), failure)
        try:
            response = await self._client.request(
                call,
                credentials.secret_key.reveal(),
                "GET",
                f"payment_intents/{payment_intent_id}",
                {"expand[]": "latest_charge"},
            )
        except ProviderTransportError:
            return QueryAttempt.retry(
                stripe_failure(
                    operation, FailureKind.AVAILABILITY, "Stripe PaymentIntent is temporarily unavailable"
                )
            )
        if response.status_code == 429:
            return QueryAttempt.retry(
                stripe_failure(
                    operation, FailureKind.RATE_LIMIT, "Stripe rate limited the PaymentIntent query"
                ),
                retry_after_seconds(response.headers),
            )
        if response.status_code >= 500:
            return QueryAttempt.retry(
                stripe_failure(
                    operation, FailureKind.AVAILABILITY, "Stripe PaymentIntent is temporarily unavailable"
                )
            )
        if response.status_code == 404:
            message = "Stripe PaymentIntent was not found"
            if not response.is_body_too_large:
                api_error = parse_stripe_api_error(response.body)
                if api_error.get("code"):
                    message += ": " + api_error["code"]
                if api_error.get("message"):
                    message += ": " + api_error["message"][:300]
            return QueryAttempt.branch_result(
                "notFound",
                empty,
                self._client.receipt(call, response, payment_intent_id),
                stripe_failure(operation, FailureKind.NOT_FOUND, message),
            )
        if not 200 <= response.status_code < 300:
            return QueryAttempt.branch_result(
                "providerRejected",
                empty,
                self._client.receipt(call, response, payment_intent_id),
                classify_stripe_rejection(operation, response),
            )
        if response.is_body_too_large:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self._client.receipt(call, response, payment_intent_id),
                stripe_failure(
                    operation, FailureKind.RESPONSE_TOO_LARGE, "Stripe response exceeds configured size limit"
                ),
            )
        try:
            payment = decode_payment_intent(response.body)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self._client.receipt(call, response, payment_intent_id),
                stripe_failure(operation, FailureKind.PROTOCOL, str(error)),
            )
        return QueryAttempt.branch_result("found", payment, self._client.receipt(call, response, payment.id))


class CreateRefundOperation:
    """Create a keyed refund and reconcile an unconfirmed server response."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> MutationDefinition:
        """Return the refund definition and stable branch identities."""
        return CREATE_REFUND_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateRefundInput) -> IdempotencyKey:
        """Use the stable connector call ID as Stripe's idempotency key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateRefundInput) -> MutationAttempt[Refund]:
        """Send one keyed refund and classify or reconcile its result."""
        operation = "createRefund"
        empty = Refund()
        try:
            values = _refund_form(input, call)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                self._client.receipt(call),
                stripe_failure(operation, FailureKind.VALIDATION, str(error)),
            )
        credentials, failure = await self._client.resolve_credentials(call, operation)
        if failure is not None or credentials is None:
            return MutationAttempt.branch_result("defect", empty, self._client.receipt(call), failure)
        try:
            response = await self._client.request(
                call, credentials.secret_key.reveal(), "POST", "refunds", values
            )
        except ProviderTransportError:
            return MutationAttempt.retry(
                stripe_failure(operation, FailureKind.TRANSPORT, "Stripe refund response is unavailable")
            )
        except ValueError:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                self._client.receipt(call),
                stripe_failure(operation, FailureKind.LOCAL_DEFECT, "Stripe request is invalid"),
            )
        if response.status_code == 429:
            return MutationAttempt.retry(
                stripe_failure(operation, FailureKind.RATE_LIMIT, "Stripe rate limited the refund request"),
                retry_after_seconds(response.headers),
            )
        if response.status_code == 409 and should_retry_keyed_conflict(response):
            return MutationAttempt.retry(
                stripe_failure(
                    operation,
                    FailureKind.CONFLICT,
                    "Stripe is still processing a request with this idempotency key",
                ),
                retry_after_seconds(response.headers),
            )
        if response.status_code >= 500:
            if should_retry_keyed_server_failure(response):
                return MutationAttempt.retry(
                    stripe_failure(
                        operation, FailureKind.AVAILABILITY, "Stripe refund is temporarily unavailable"
                    ),
                    retry_after_seconds(response.headers),
                )
            return await self._read_back_refund(
                call, credentials.secret_key.reveal(), input.payment_intent_id, response
            )
        if not 200 <= response.status_code < 300:
            return MutationAttempt.branch_result(
                "providerRejected", empty, self._client.receipt(call, response), _refund_rejection(response)
            )
        if response.is_body_too_large:
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                self._client.receipt(call, response),
                stripe_failure(
                    operation, FailureKind.RESPONSE_TOO_LARGE, "Stripe response exceeds configured size limit"
                ),
            )
        try:
            refund = decode_refund(response.body)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                self._client.receipt(call, response),
                stripe_failure(operation, FailureKind.PROTOCOL, str(error)),
            )
        return MutationAttempt.branch_result(
            "refunded", refund, self._client.receipt(call, response, refund.refund_id)
        )

    async def _read_back_refund(
        self, call: Call, secret_key: str, payment_intent_id: str, original: ProviderResponse
    ) -> MutationAttempt[Refund]:
        query = {"payment_intent": payment_intent_id, "limit": "100"}
        for _ in range(3):
            try:
                response = await self._client.request(call, secret_key, "GET", "refunds", query)
            except ProviderTransportError:
                return MutationAttempt.retry(
                    stripe_failure(
                        "createRefund",
                        FailureKind.AVAILABILITY,
                        "Stripe refund read-back is temporarily unavailable",
                    )
                )
            if response.status_code == 429 or response.status_code >= 500:
                return MutationAttempt.retry(
                    stripe_failure(
                        "createRefund",
                        FailureKind.AVAILABILITY,
                        "Stripe refund read-back is temporarily unavailable",
                    ),
                    retry_after_seconds(response.headers),
                )
            if response.status_code in (401, 403):
                return self._denied_refund_readback(call, original, response.status_code)
            if not 200 <= response.status_code < 300 or response.is_body_too_large:
                break
            try:
                listing = json.loads(response.body)
            except (ValueError, UnicodeDecodeError):
                break
            if (
                not isinstance(listing, dict)
                or listing.get("object") != "list"
                or not isinstance(listing.get("data"), list)
                or not isinstance(listing.get("has_more"), bool)
                or len(listing["data"]) > 100
            ):
                break
            last_refund_id = ""
            for candidate in listing["data"]:
                if (
                    not isinstance(candidate, dict)
                    or not isinstance(candidate.get("id"), str)
                    or not candidate["id"].startswith("re_")
                ):
                    return self._uncertain_refund(call, original)
                last_refund_id = candidate["id"]
                metadata = candidate.get("metadata")
                if metadata is not None and (
                    not isinstance(metadata, dict)
                    or not all(
                        isinstance(key, str) and isinstance(value, str) for key, value in metadata.items()
                    )
                ):
                    return self._uncertain_refund(call, original)
                if isinstance(metadata, dict) and metadata.get("dex_call_id") == str(call.id):
                    try:
                        refund = decode_refund(json.dumps(candidate).encode())
                    except ValueError:
                        return self._uncertain_refund(call, original)
                    return MutationAttempt.branch_result(
                        "refunded", refund, self._client.receipt(call, response, refund.refund_id)
                    )
            if not listing["has_more"] or not last_refund_id:
                break
            query["starting_after"] = last_refund_id
        return self._uncertain_refund(call, original)

    def _uncertain_refund(self, call: Call, original: ProviderResponse) -> MutationAttempt[Refund]:
        return MutationAttempt(
            "uncertain",
            "uncertain",
            Refund(),
            self._client.receipt(call, original),
            stripe_failure(
                "createRefund",
                FailureKind.AVAILABILITY,
                "Stripe refund outcome cannot be confirmed after a server error",
            ),
        )

    def _denied_refund_readback(
        self, call: Call, original: ProviderResponse, status_code: int
    ) -> MutationAttempt[Refund]:
        if status_code == 401:
            kind = FailureKind.AUTHENTICATION
            message = (
                "Stripe refund outcome cannot be confirmed after a server error because Stripe rejected "
                "the API key for the refund read-back; check that the key is still valid"
            )
        else:
            kind = FailureKind.AUTHORIZATION
            message = (
                "Stripe refund outcome cannot be confirmed after a server error because the API key lacks "
                "permission for the refund read-back; grant it Refund read permission"
            )
        return MutationAttempt(
            "uncertain",
            "uncertain",
            Refund(),
            self._client.receipt(call, original),
            stripe_failure("createRefund", kind, message),
        )
