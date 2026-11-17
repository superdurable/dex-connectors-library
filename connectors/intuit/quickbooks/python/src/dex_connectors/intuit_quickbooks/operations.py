# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""QuickBooks operation invokes and Go-compatible Dex outcomes."""

import dataclasses
import json
import re
from collections.abc import Awaitable, Callable
from datetime import UTC
from decimal import ROUND_HALF_UP, Decimal
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
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    Decision,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated import definitions as definitions
from .client import Client, Exchange, _failure
from .models import (
    CreateCustomerInput,
    CreateCustomerOutput,
    CreateInvoiceInput,
    CreateInvoiceOutput,
    Customer,
    FindCustomerInput,
    FindCustomerOutput,
    GetInvoiceInput,
    Invoice,
    InvoiceLineInput,
    InvoicePage,
    ListInvoicesInput,
    Payment,
    RecordPaymentInput,
    SendInvoiceInput,
)
from .provider import (
    decode_customer,
    decode_invoice,
    decode_payment,
    entity,
    parse_timestamp,
    query_entities,
    query_literal,
    validate_currency,
    validate_date,
    validate_decimal,
    validate_email,
    validate_id,
    validate_text,
)


def _query_result(
    client: Client,
    call: Call,
    operation: str,
    exchange: Exchange,
    empty: Any,
    not_found: bool = False,
    object_id: str = "",
) -> QueryAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    if exchange.outcome in ("retry", "interrupted"):
        assert exchange.failure is not None
        return QueryAttempt.retry(exchange.failure, exchange.retry_after)
    branch = (
        "defect"
        if exchange.outcome == "defect"
        else "invalidResponse"
        if exchange.outcome == "invalid"
        else "notFound"
        if exchange.outcome == "notFound" and not_found
        else "providerRejected"
    )
    return QueryAttempt.branch_result(
        branch, empty, client.receipt(call, exchange, object_id), exchange.failure
    )


def _invalid_query(
    operation: str,
    client: Client,
    call: Call,
    exchange: Exchange,
    empty: Any,
    message: str,
    object_id: str = "",
) -> QueryAttempt[Any]:
    return QueryAttempt.branch_result(
        "invalidResponse",
        empty,
        client.receipt(call, exchange, object_id),
        _failure(operation, FailureKind.PROTOCOL, message),
    )


def _json_body(value: dict[str, Any]) -> bytes:
    return json.dumps(value, separators=(",", ":"), ensure_ascii=False).encode()


def _created_customer_matches(customer: Customer, payload: dict[str, Any]) -> bool:
    return (
        customer.display_name == payload["DisplayName"]
        and all(
            not payload.get(name) or getattr(customer, field) == payload[name]
            for name, field in (
                ("GivenName", "given_name"),
                ("FamilyName", "family_name"),
                ("CompanyName", "company_name"),
            )
        )
        and (
            "PrimaryEmailAddr" not in payload
            or customer.email_address.lower() == payload["PrimaryEmailAddr"]["Address"].lower()
        )
    )


# Statuses that prove a write was refused; 409, 501, 3xx, and other statuses are unknown.
_DEFINITE_REFUSAL_STATUSES = frozenset((400, 401, 403, 404, 411, 413, 414, 415, 422))

# Confirms a read entity body as this Step's write, returning the happy-path output or None.
_Confirm = Callable[[bytes, str], Any | None]


def _accepted_entity_id(body: bytes, entity_name: str) -> str:
    """Extract only an entity ID from a 2xx answer that failed full validation."""
    try:
        return validate_id("Id", entity(body, entity_name).get("Id", ""))
    except (ValueError, TypeError, KeyError, AttributeError):
        return ""


def _is_write_outcome_unknown(exchange: Exchange) -> bool:
    """Report an outcome after a send that does not prove QuickBooks refused the write."""
    if exchange.outcome == "retry":
        return True
    # A Fault at 2xx or 400 is a definite business refusal; other unlisted statuses are unknown.
    return (
        exchange.outcome == "rejected"
        and exchange.status not in _DEFINITE_REFUSAL_STATUSES
        and not 200 <= exchange.status < 300
    )


class _FencedWrite:
    """One QuickBooks write attempt under a single-dispatch fence capped by call.max_sends."""

    def __init__(
        self,
        client: Client,
        call: Call,
        operation: str,
        fence: Fence,
        decision: Decision,
        requested: Any,
        happy: str,
        entity_path: str,
        confirm: _Confirm,
        has_name_conflict: bool,
    ) -> None:
        self._client = client
        self._call = call
        self._operation = operation
        self._fence = fence
        self._requested = requested
        self._happy = happy
        self._entity_path = entity_path
        self._confirm = confirm
        self._has_name_conflict = has_name_conflict
        # False when this attempt's send is the last one the cap allows.
        self._can_resend_unknown_outcome = decision.prior_send_upper_bound + 1 < call.max_sends
        # A refusal of this send cannot prove nothing was written.
        self._has_possible_earlier_send = decision.prior_send_upper_bound > 0

    async def dispatch(self, send: Callable[[], Awaitable[Exchange]]) -> Exchange | MutationAttempt[Any]:
        """Send once under the fence; an attempt result means nothing was sent."""
        try:
            return await self._fence.dispatch(lambda _deadline: send())
        except CheckpointError:
            message = "Dex did not record the dispatch checkpoint; nothing was sent in this attempt"
        except (InsufficientAttemptTimeError, DispatchNotAllowedError):
            message = "too little of the Step deadline remained to send; nothing was sent in this attempt"
        except TimeoutError:
            return Exchange(
                "retry",
                _failure(
                    self._operation,
                    FailureKind.TRANSPORT,
                    "QuickBooks request failed before a response arrived",
                ),
            )
        return MutationAttempt.retry(_failure(self._operation, FailureKind.AVAILABILITY, message))

    async def attempt_for_exchange(self, exchange: Exchange, receipt: Receipt) -> MutationAttempt[Any] | None:
        """Map every outcome except success and an accepted but unusable 2xx."""
        if exchange.outcome in ("succeeded", "invalid", "interrupted"):
            return None
        assert exchange.failure is not None
        if exchange.outcome == "retry" and (exchange.provably_unsent or exchange.status in (429, 503)):
            try:
                await self._fence.record_can_resend()
            except Exception:
                return MutationAttempt.retry(
                    _failure(
                        self._operation,
                        FailureKind.AVAILABILITY,
                        "the QuickBooks retry checkpoint could not be recorded",
                    )
                )
            return MutationAttempt.retry(exchange.failure, exchange.retry_after)
        if not exchange.provably_unsent and _is_write_outcome_unknown(exchange):
            return self._resend_or_select_uncertain(exchange.failure, exchange.retry_after, receipt)
        try:
            await self._fence.record_can_resend()
        except Exception:
            pass  # The refusal proves nothing was written; a lost checkpoint only makes a replay count it.
        branch = (
            "defect"
            if exchange.outcome == "defect"
            else "nameConflict"
            if exchange.outcome == "nameConflict" and self._has_name_conflict
            else "providerRejected"
        )
        if self._has_possible_earlier_send:
            return MutationAttempt.uncertain(
                self._requested,
                _failure(
                    self._operation,
                    exchange.failure.kind,
                    "an earlier attempt may have written to QuickBooks, so this refusal cannot prove "
                    "nothing was written: " + exchange.failure.message,
                ),
                receipt,
            )
        return MutationAttempt.branch_result(branch, self._requested, receipt, exchange.failure)

    async def unusable_accepted(self, entity_id: str, receipt: Receipt, reason: str) -> MutationAttempt[Any]:
        """Read back an accepted 2xx that failed validation and otherwise treat it as unknown."""
        unknown = _failure(
            self._operation,
            FailureKind.PROTOCOL,
            "QuickBooks accepted the write but returned an unusable answer: " + reason,
        )
        if not entity_id:
            return self._resend_or_select_uncertain(unknown, 0, receipt)
        try:
            await self._fence.record_provider_id(entity_id)
        except Exception:
            pass  # The read-back below still runs; a later attempt without the ID counts this send.
        result = await self._client.exchange(
            self._call, self._operation, "GET", f"/{self._entity_path}/{entity_id}"
        )
        if result.outcome == "succeeded":
            output = self._confirm(result.body, entity_id)
            if output is not None:
                return MutationAttempt.branch_result(
                    self._happy, output, dataclasses.replace(receipt, provider_object_id=entity_id)
                )
        return self._resend_or_select_uncertain(unknown, 0, receipt)

    async def read_back_only(self, entity_id: str) -> MutationAttempt[Any]:
        """Confirm an accepted earlier write by ID; it never sends."""
        result = await self._client.exchange(
            self._call, self._operation, "GET", f"/{self._entity_path}/{entity_id}"
        )
        receipt = self._client.receipt(self._call, result, entity_id)
        if result.outcome in ("retry", "interrupted"):
            # Reading is harmless to repeat, and a retry of this path never sends.
            assert result.failure is not None
            return MutationAttempt.retry(result.failure, result.retry_after)
        if result.outcome == "succeeded":
            output = self._confirm(result.body, entity_id)
            if output is not None:
                return MutationAttempt.branch_result(self._happy, output, receipt)
        return MutationAttempt.uncertain(
            self._requested,
            _failure(
                self._operation,
                FailureKind.PROTOCOL,
                f"QuickBooks accepted an earlier attempt's write, but reading entity {entity_id} back "
                "could not confirm it",
            ),
            receipt,
        )

    def _resend_or_select_uncertain(
        self, unknown: Failure, retry_after: float, receipt: Receipt
    ) -> MutationAttempt[Any]:
        if self._can_resend_unknown_outcome:
            # The claim stays the last marker, so the next attempt counts this send.
            return MutationAttempt.retry(unknown, retry_after)
        return MutationAttempt.uncertain(self._requested, unknown, receipt)


async def _begin_fenced_write(
    client: Client,
    call: Call,
    operation: str,
    definition: MutationDefinition,
    requested: Any,
    happy: str,
    entity_path: str,
    confirm: _Confirm,
    *,
    has_name_conflict: bool = False,
) -> _FencedWrite | MutationAttempt[Any]:
    """Decide the attempt before credentials; an attempt result means it needs no send."""
    try:
        fence = Fence(call, definition.step_defaults.execute_method_timeout, max_sends=call.max_sends)
        decision = await fence.decide()
    except ValueError:
        return MutationAttempt.branch_result(
            "defect",
            requested,
            Receipt(),
            _failure(
                operation, FailureKind.LOCAL_DEFECT, "QuickBooks dispatch fence could not be constructed"
            ),
        )
    write = _FencedWrite(
        client, call, operation, fence, decision, requested, happy, entity_path, confirm, has_name_conflict
    )
    if decision.kind == DecisionKind.FAIL_CLOSED:
        return MutationAttempt.retry(
            _failure(
                operation,
                FailureKind.AVAILABILITY,
                "the dispatch checkpoint could not be read; nothing was sent in this attempt",
            )
        )
    if decision.marker is not None and decision.marker.provider_id:
        # QuickBooks accepted an earlier send with this ID, so it is read back and never resent.
        return await write.read_back_only(decision.marker.provider_id)
    if decision.kind == DecisionKind.READ_BACK_REQUIRED:
        return MutationAttempt.uncertain(
            requested,
            _failure(
                operation,
                FailureKind.TRANSPORT,
                "an earlier attempt may have written to QuickBooks and the send cap is reached",
            ),
            Receipt(),
        )
    return write


class FindCustomerOperation:
    """Query QuickBooks Customer by exact email address or display name."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated findCustomer definition."""
        return definitions.FIND_CUSTOMER_DEFINITION

    async def invoke(self, call: Call, input: FindCustomerInput) -> QueryAttempt[FindCustomerOutput]:
        """Run one Customer query and distinguish zero, one, and several matches."""
        email = input.email_address.strip()
        name = input.display_name.strip()
        requested = FindCustomerOutput(email_address=email, display_name=name)
        try:
            if email and name:
                raise ValueError("give either emailAddress or displayName, not both")
            if email:
                validate_email("emailAddress", email)
                condition = "PrimaryEmailAddr = " + query_literal("emailAddress", email, 254)
            elif name:
                condition = "DisplayName = " + query_literal("displayName", name, 500)
            else:
                raise ValueError("emailAddress or displayName is required")
            if input.includes_inactive:
                condition += " AND Active IN (true, false)"
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", requested, Receipt(), _failure("findCustomer", FailureKind.VALIDATION, str(error))
            )
        statement = "select * from Customer where " + condition + " MAXRESULTS 10"
        exchange = await self.client.exchange(call, "findCustomer", "GET", "/query", {"query": statement})
        terminal = _query_result(self.client, call, "findCustomer", exchange, requested)
        if terminal is not None:
            return terminal
        try:
            matches = []
            for item in query_entities(exchange.body, "Customer"):
                customer = decode_customer(item)
                if not input.includes_inactive and not customer.is_active:
                    continue
                if email and customer.email_address.lower() != email.lower():
                    continue
                if name and customer.display_name.lower() != name.lower():
                    continue
                matches.append(customer)
                if len(matches) > 10:
                    raise ValueError("too many matching customers")
        except (ValueError, TypeError, KeyError) as error:
            return _invalid_query(
                "findCustomer",
                self.client,
                call,
                exchange,
                requested,
                "QuickBooks returned an invalid customer list: " + str(error),
            )
        receipt = self.client.receipt(call, exchange)
        if not matches:
            return QueryAttempt.branch_result(
                "notFound",
                requested,
                receipt,
                _failure("findCustomer", FailureKind.NOT_FOUND, "no QuickBooks customer matches"),
            )
        if len(matches) == 1:
            receipt = self.client.receipt(call, exchange, matches[0].customer_id)
            return QueryAttempt.branch_result(
                "found",
                FindCustomerOutput(email_address=email, display_name=name, customer=matches[0]),
                receipt,
            )
        return QueryAttempt.branch_result(
            "ambiguous",
            FindCustomerOutput(email_address=email, display_name=name, matches=tuple(matches)),
            receipt,
            _failure("findCustomer", FailureKind.CONFLICT, "several QuickBooks customers match"),
        )


class GetInvoiceOperation:
    """Read one QuickBooks Invoice by entity ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated getInvoice definition."""
        return definitions.GET_INVOICE_DEFINITION

    async def invoke(self, call: Call, input: GetInvoiceInput) -> QueryAttempt[Invoice]:
        """Read and validate one invoice without changing it."""
        identifier = input.invoice_id.strip()
        requested = Invoice(invoice_id=identifier)
        try:
            validate_id("invoiceId", identifier)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", requested, Receipt(), _failure("getInvoice", FailureKind.VALIDATION, str(error))
            )
        exchange = await self.client.exchange(call, "getInvoice", "GET", "/invoice/" + identifier)
        terminal = _query_result(self.client, call, "getInvoice", exchange, requested, True, identifier)
        if terminal is not None:
            return terminal
        try:
            invoice = decode_invoice(entity(exchange.body, "Invoice"))
            if invoice.invoice_id != identifier:
                raise ValueError("QuickBooks returned another invoice")
        except (ValueError, TypeError, KeyError) as error:
            return _invalid_query(
                "getInvoice",
                self.client,
                call,
                exchange,
                requested,
                "QuickBooks returned an invalid invoice: " + str(error),
                identifier,
            )
        return QueryAttempt.branch_result("found", invoice, self.client.receipt(call, exchange, identifier))


class ListInvoicesOperation:
    """Read one bounded QuickBooks Invoice query page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated listInvoices definition."""
        return definitions.LIST_INVOICES_DEFINITION

    async def invoke(self, call: Call, input: ListInvoicesInput) -> QueryAttempt[InvoicePage]:
        """Query invoices in stable creation order with bounded pagination."""
        empty = InvoicePage()
        try:
            start = input.start_position or 1
            maximum = input.max_results or 25
            if start < 1:
                raise ValueError("startPosition must be 1 or greater")
            if not 1 <= maximum <= 100:
                raise ValueError("maxResults must be between 1 and 100")
            conditions = []
            customer = input.customer_id.strip()
            doc = input.doc_number.strip()
            if customer:
                conditions.append(
                    "CustomerRef = " + query_literal("customerId", validate_id("customerId", customer), 20)
                )
            if doc:
                conditions.append("DocNumber = " + query_literal("docNumber", doc, 21))
            if input.is_open_only:
                conditions.append("Balance > '0'")
            for field, value, operator in (
                ("txnDateFrom", input.txn_date_from, ">="),
                ("txnDateTo", input.txn_date_to, "<="),
            ):
                if value.strip():
                    conditions.append(
                        "TxnDate "
                        + operator
                        + " "
                        + query_literal(field, validate_date(field, value.strip()), 10)
                    )
            if input.updated_since is not None:
                updated = (
                    parse_timestamp(input.updated_since)
                    if isinstance(input.updated_since, str)
                    else input.updated_since.astimezone(UTC)
                )
                if updated is None:
                    raise ValueError("updatedSince must be a valid timestamp")
                instant = updated.strftime("%Y-%m-%dT%H:%M:%SZ")
                conditions.append("MetaData.LastUpdatedTime >= " + query_literal("updatedSince", instant, 30))
            statement = "select * from Invoice"
            if conditions:
                statement += " where " + " AND ".join(conditions)
            statement += f" ORDERBY MetaData.CreateTime STARTPOSITION {start} MAXRESULTS {maximum}"
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), _failure("listInvoices", FailureKind.VALIDATION, str(error))
            )
        exchange = await self.client.exchange(call, "listInvoices", "GET", "/query", {"query": statement})
        terminal = _query_result(self.client, call, "listInvoices", exchange, empty)
        if terminal is not None:
            return terminal
        try:
            elements = query_entities(exchange.body, "Invoice")
            if len(elements) > maximum:
                raise ValueError("response has more invoices than requested")
            invoices = tuple(decode_invoice(value) for value in elements)
            has_more = len(invoices) == maximum
            page = InvoicePage(
                invoices=invoices,
                start_position=start,
                max_results=maximum,
                has_more_pages=has_more,
                next_start_position=start + maximum if has_more else 0,
            )
        except (ValueError, TypeError, KeyError) as error:
            return _invalid_query(
                "listInvoices",
                self.client,
                call,
                exchange,
                empty,
                "QuickBooks returned an invalid invoice page: " + str(error),
            )
        return QueryAttempt.branch_result("listed", page, self.client.receipt(call, exchange))


class CreateCustomerOperation:
    """Create one QuickBooks Customer under a stable requestid."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated createCustomer definition."""
        return definitions.CREATE_CUSTOMER_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateCustomerInput) -> IdempotencyKey:
        """Use the stable Dex Call ID as QuickBooks requestid."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateCustomerInput) -> MutationAttempt[CreateCustomerOutput]:
        """Create or replay the customer, confirming an unusable accepted response by ID."""
        display_name = input.display_name.strip()
        requested = CreateCustomerOutput(display_name=display_name)
        try:
            if not display_name:
                raise ValueError("displayName is required")
            payload: dict[str, Any] = {"DisplayName": validate_text("displayName", display_name, 500)}
            if ":" in display_name:
                raise ValueError(
                    "displayName cannot contain a colon, which QuickBooks reserves for sub-customers"
                )
            for source, target in (
                ("given_name", "GivenName"),
                ("family_name", "FamilyName"),
                ("company_name", "CompanyName"),
            ):
                value = getattr(input, source).strip()
                if value:
                    payload[target] = validate_text(source, value, 100)
            email = input.email_address.strip()
            if email:
                payload["PrimaryEmailAddr"] = {
                    "Address": validate_text("emailAddress", validate_email("emailAddress", email), 100)
                }
            phone = input.phone.strip()
            if phone:
                payload["PrimaryPhone"] = {"FreeFormNumber": validate_text("phone", phone, 30)}
            currency = input.currency_code.strip()
            if currency:
                payload["CurrencyRef"] = {"value": validate_currency("currencyCode", currency)}
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), _failure("createCustomer", FailureKind.VALIDATION, str(error))
            )

        def confirm(body: bytes, customer_id: str) -> CreateCustomerOutput | None:
            try:
                read = decode_customer(entity(body, "Customer"))
            except (ValueError, TypeError, KeyError):
                return None
            if read.customer_id != customer_id or not _created_customer_matches(read, payload):
                return None
            return CreateCustomerOutput(customer=read, display_name=display_name)

        write = await _begin_fenced_write(
            self.client,
            call,
            "createCustomer",
            definitions.CREATE_CUSTOMER_DEFINITION,
            requested,
            "created",
            "customer",
            confirm,
            has_name_conflict=True,
        )
        if isinstance(write, MutationAttempt):
            return write
        exchange = await write.dispatch(
            lambda: self.client.exchange(
                call, "createCustomer", "POST", "/customer", payload=_json_body(payload), mutation=True
            )
        )
        if isinstance(exchange, MutationAttempt):
            return exchange
        receipt = self.client.receipt(call, exchange)
        terminal = await write.attempt_for_exchange(exchange, receipt)
        if terminal is not None:
            return terminal
        try:
            customer = decode_customer(entity(exchange.body, "Customer"))
            if not _created_customer_matches(customer, payload):
                raise ValueError("QuickBooks returned another customer")
        except (ValueError, TypeError, KeyError) as error:
            return await write.unusable_accepted(
                _accepted_entity_id(exchange.body, "Customer"), receipt, str(error)
            )
        return MutationAttempt.branch_result(
            "created",
            CreateCustomerOutput(customer=customer, display_name=display_name),
            self.client.receipt(call, exchange, customer.customer_id),
        )


class CreateInvoiceOperation:
    """Create one QuickBooks Invoice under a stable requestid."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated createInvoice definition."""
        return definitions.CREATE_INVOICE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateInvoiceInput) -> IdempotencyKey:
        """Use the stable Dex Call ID as QuickBooks requestid."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateInvoiceInput) -> MutationAttempt[CreateInvoiceOutput]:
        """Create an exact-decimal invoice and confirm accepted partial responses by ID."""
        document_number = input.doc_number.strip()
        requested = CreateInvoiceOutput(doc_number=document_number)
        try:
            customer_id = validate_id("customerId", input.customer_id.strip())
            payload: dict[str, Any] = {"CustomerRef": {"value": customer_id}, "Line": []}
            for field, target in (("txn_date", "TxnDate"), ("due_date", "DueDate")):
                value = getattr(input, field).strip()
                if value:
                    payload[target] = validate_date(field, value)
            if input.txn_date and input.due_date and input.due_date.strip() < input.txn_date.strip():
                raise ValueError("dueDate cannot be before txnDate")
            query_literal("docNumber", document_number, 21)
            if document_number:
                payload["DocNumber"] = document_number
            if input.bill_email.strip():
                payload["BillEmail"] = {"Address": validate_email("billEmail", input.bill_email.strip())}
            if input.currency_code.strip():
                payload["CurrencyRef"] = {
                    "value": validate_currency("currencyCode", input.currency_code.strip())
                }
            if input.customer_memo:
                payload["CustomerMemo"] = {
                    "value": validate_text("customerMemo", input.customer_memo, 1000, True)
                }
            if input.private_note:
                payload["PrivateNote"] = validate_text("privateNote", input.private_note, 4000, True)
            if not 1 <= len(input.lines) <= 100:
                raise ValueError("lines must hold 1 to 100 lines")
            for index, line in enumerate(input.lines):
                if isinstance(line, dict):
                    line = InvoiceLineInput(
                        item_id=line.get("itemId", ""),
                        description=line.get("description", ""),
                        quantity=line.get("quantity", ""),
                        unit_price=line.get("unitPrice", ""),
                        amount=line.get("amount", ""),
                        tax_code_id=line.get("taxCodeId", ""),
                        service_date=line.get("serviceDate", ""),
                    )
                field = f"lines[{index}]"
                item_id = validate_id(field + ".itemId", line.item_id.strip())
                detail: dict[str, Any] = {"ItemRef": {"value": item_id}}
                if line.service_date.strip():
                    detail["ServiceDate"] = validate_date(field + ".serviceDate", line.service_date.strip())
                if line.tax_code_id.strip():
                    tax = line.tax_code_id.strip()
                    if not re.fullmatch(r"[A-Za-z0-9]{1,20}", tax):
                        raise ValueError(
                            field + ".taxCodeId must be a QuickBooks tax code such as TAX, NON, or 3"
                        )
                    detail["TaxCodeRef"] = {"value": tax}
                if line.quantity:
                    detail["Qty"] = validate_decimal(field + ".quantity", line.quantity, 12, 5)
                if line.unit_price:
                    detail["UnitPrice"] = validate_decimal(
                        field + ".unitPrice", line.unit_price, 12, 5, allow_negative=True
                    )
                amount = line.amount
                if not amount:
                    if not line.quantity or not line.unit_price:
                        raise ValueError(field + " needs an amount, or both quantity and unitPrice")
                    amount = format(
                        (Decimal(line.quantity) * Decimal(line.unit_price)).quantize(
                            Decimal("0.01"), rounding=ROUND_HALF_UP
                        ),
                        "f",
                    )
                validate_decimal(field + ".amount", amount, 10, 2, allow_negative=True)
                item_line: dict[str, Any] = {
                    "DetailType": "SalesItemLineDetail",
                    "Amount": amount,
                    "SalesItemLineDetail": detail,
                }
                if line.description:
                    item_line["Description"] = validate_text(
                        field + ".description", line.description, 4000, True
                    )
                payload["Line"].append(item_line)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), _failure("createInvoice", FailureKind.VALIDATION, str(error))
            )
        encoded = _json_body(payload)
        encoded = re.sub(rb'"(Amount|Qty|UnitPrice)":"(-?[0-9]+(?:\.[0-9]+)?)"', rb'"\1":\2', encoded)

        def confirm(body: bytes, invoice_id: str) -> CreateInvoiceOutput | None:
            try:
                read = decode_invoice(entity(body, "Invoice"))
            except (ValueError, TypeError, KeyError):
                return None
            if (
                read.invoice_id != invoice_id
                or read.customer_id != customer_id
                or (document_number and read.doc_number != document_number)
            ):
                return None
            return CreateInvoiceOutput(invoice=read, doc_number=document_number)

        write = await _begin_fenced_write(
            self.client,
            call,
            "createInvoice",
            definitions.CREATE_INVOICE_DEFINITION,
            requested,
            "created",
            "invoice",
            confirm,
        )
        if isinstance(write, MutationAttempt):
            return write
        exchange = await write.dispatch(
            lambda: self.client.exchange(
                call, "createInvoice", "POST", "/invoice", payload=encoded, mutation=True
            )
        )
        if isinstance(exchange, MutationAttempt):
            return exchange
        receipt = self.client.receipt(call, exchange)
        terminal = await write.attempt_for_exchange(exchange, receipt)
        if terminal is not None:
            return terminal
        try:
            invoice = decode_invoice(entity(exchange.body, "Invoice"))
            if invoice.customer_id != customer_id:
                raise ValueError("invoice belongs to another customer")
        except (ValueError, TypeError, KeyError) as error:
            return await write.unusable_accepted(
                _accepted_entity_id(exchange.body, "Invoice"), receipt, str(error)
            )
        return MutationAttempt.branch_result(
            "created",
            CreateInvoiceOutput(invoice=invoice, doc_number=document_number),
            self.client.receipt(call, exchange, invoice.invoice_id),
        )


class SendInvoiceOperation:
    """Email an invoice using QuickBooks's send endpoint."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated sendInvoice definition."""
        return definitions.SEND_INVOICE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SendInvoiceInput) -> IdempotencyKey:
        """Use the stable Dex Call ID as QuickBooks requestid."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: SendInvoiceInput) -> MutationAttempt[Invoice]:
        """Send an invoice and confirm accepted unusable responses by ID."""
        identifier = input.invoice_id.strip()
        send_to = input.send_to.strip()
        requested = Invoice(invoice_id=identifier)
        try:
            validate_id("invoiceId", identifier)
            if send_to:
                validate_email("sendTo", send_to)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), _failure("sendInvoice", FailureKind.VALIDATION, str(error))
            )

        def confirm(body: bytes, invoice_id: str) -> Invoice | None:
            try:
                read = decode_invoice(entity(body, "Invoice"))
            except (ValueError, TypeError, KeyError):
                return None
            if (
                read.invoice_id != invoice_id
                or read.email_status != "EmailSent"
                or (send_to and read.bill_email.lower() != send_to.lower())
            ):
                return None
            return read

        write = await _begin_fenced_write(
            self.client,
            call,
            "sendInvoice",
            definitions.SEND_INVOICE_DEFINITION,
            requested,
            "sent",
            "invoice",
            confirm,
        )
        if isinstance(write, MutationAttempt):
            return write
        exchange = await write.dispatch(
            lambda: self.client.exchange(
                call,
                "sendInvoice",
                "POST",
                "/invoice/" + identifier + "/send",
                {"sendTo": send_to} if send_to else None,
                mutation=True,
            )
        )
        if isinstance(exchange, MutationAttempt):
            return exchange
        receipt = self.client.receipt(call, exchange, identifier)
        terminal = await write.attempt_for_exchange(exchange, receipt)
        if terminal is not None:
            return terminal
        try:
            invoice = decode_invoice(entity(exchange.body, "Invoice"))
            if invoice.invoice_id != identifier:
                raise ValueError("QuickBooks returned another invoice")
            if invoice.email_status != "EmailSent":
                raise ValueError("the returned invoice is not marked EmailSent")
            if send_to and invoice.bill_email.lower() != send_to.lower():
                raise ValueError("the returned invoice has another billing address")
        except (ValueError, TypeError, KeyError) as error:
            return await write.unusable_accepted(identifier, receipt, str(error))
        return MutationAttempt.branch_result("sent", invoice, receipt)


class RecordPaymentOperation:
    """Record a received payment applied to one QuickBooks invoice."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated recordPayment definition."""
        return definitions.RECORD_PAYMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: RecordPaymentInput) -> IdempotencyKey:
        """Use the stable Dex Call ID as QuickBooks requestid."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: RecordPaymentInput) -> MutationAttempt[Payment]:
        """Apply an exact amount and confirm an accepted partial payment by ID."""
        customer_id = input.customer_id.strip()
        invoice_id = input.invoice_id.strip()
        requested = Payment(customer_id=customer_id, total_amount=input.amount)
        try:
            validate_id("customerId", customer_id)
            validate_id("invoiceId", invoice_id)
            validate_decimal("amount", input.amount, 10, 2, positive=True)
            payload: dict[str, Any] = {
                "CustomerRef": {"value": customer_id},
                "TotalAmt": input.amount,
                "Line": [
                    {"Amount": input.amount, "LinkedTxn": [{"TxnId": invoice_id, "TxnType": "Invoice"}]}
                ],
            }
            if input.txn_date.strip():
                payload["TxnDate"] = validate_date("txnDate", input.txn_date.strip())
            if input.payment_reference_number.strip():
                payload["PaymentRefNum"] = validate_text(
                    "paymentReferenceNumber", input.payment_reference_number.strip(), 21
                )
            if input.private_note:
                payload["PrivateNote"] = validate_text("privateNote", input.private_note, 4000, True)
            for field, target in (
                ("deposit_account_id", "DepositToAccountRef"),
                ("payment_method_id", "PaymentMethodRef"),
            ):
                value = getattr(input, field).strip()
                if value:
                    payload[target] = {"value": validate_id(field, value)}
            if input.currency_code.strip():
                payload["CurrencyRef"] = {
                    "value": validate_currency("currencyCode", input.currency_code.strip())
                }
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), _failure("recordPayment", FailureKind.VALIDATION, str(error))
            )
        encoded = _json_body(payload)
        encoded = re.sub(rb'"(Amount|TotalAmt)":"(-?[0-9]+(?:\.[0-9]+)?)"', rb'"\1":\2', encoded)

        def confirm(body: bytes, payment_id: str) -> Payment | None:
            try:
                read = decode_payment(entity(body, "Payment"))
                is_amount_equal = Decimal(read.total_amount) == Decimal(input.amount)
            except (ValueError, TypeError, KeyError, ArithmeticError):
                return None
            if (
                read.payment_id != payment_id
                or read.customer_id != customer_id
                or not is_amount_equal
                or not any(application.invoice_id == invoice_id for application in read.applied_invoices)
            ):
                return None
            return read

        write = await _begin_fenced_write(
            self.client,
            call,
            "recordPayment",
            definitions.RECORD_PAYMENT_DEFINITION,
            requested,
            "recorded",
            "payment",
            confirm,
        )
        if isinstance(write, MutationAttempt):
            return write
        exchange = await write.dispatch(
            lambda: self.client.exchange(
                call, "recordPayment", "POST", "/payment", payload=encoded, mutation=True
            )
        )
        if isinstance(exchange, MutationAttempt):
            return exchange
        receipt = self.client.receipt(call, exchange)
        terminal = await write.attempt_for_exchange(exchange, receipt)
        if terminal is not None:
            return terminal
        try:
            payment = decode_payment(entity(exchange.body, "Payment"))
            if payment.customer_id != customer_id or not any(
                application.invoice_id == invoice_id for application in payment.applied_invoices
            ):
                raise ValueError("the returned payment does not apply to the invoice")
        except (ValueError, TypeError, KeyError) as error:
            return await write.unusable_accepted(
                _accepted_entity_id(exchange.body, "Payment"), receipt, str(error)
            )
        return MutationAttempt.branch_result(
            "recorded", payment, self.client.receipt(call, exchange, payment.payment_id)
        )
