# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Xero operation invocation, validation, and exact-decimal provider decoding."""

from __future__ import annotations

import ipaddress
import json
import re
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from decimal import Decimal as ExactDecimal
from email.utils import parseaddr
from typing import Any, TypeVar

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
    CREATE_INVOICE_DEFINITION,
    FIND_CONTACT_BY_EMAIL_DEFINITION,
    GET_INVOICE_DEFINITION,
    LIST_CONTACTS_DEFINITION,
    LIST_INVOICES_DEFINITION,
    RECORD_PAYMENT_DEFINITION,
)
from .client import Client, Exchange, equal_fold, failure, go_path_escape, is_uuid
from .models import (
    Contact,
    ContactPage,
    CreateInvoiceInput,
    CreateInvoiceOutput,
    FindContactByEmailInput,
    FindContactByEmailOutput,
    GetInvoiceInput,
    Invoice,
    InvoiceContact,
    InvoiceLineItem,
    InvoiceLineItemInput,
    InvoicePage,
    InvoicePayment,
    ListContactsInput,
    ListInvoicesInput,
    Payment,
    PaymentAccount,
    PaymentInvoice,
    RecordPaymentInput,
    RecordPaymentOutput,
)

_DECIMAL = re.compile(r"-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,10})?\Z")
_CODE = re.compile(r"[A-Za-z0-9][A-Za-z0-9 ._/-]{0,49}\Z")
_DOTNET = re.compile(r"/Date\((-?[0-9]{1,15})(?:[+-][0-9]{4})?\)/\Z")
_ZERO_TIME = datetime.min.replace(tzinfo=UTC)
_T = TypeVar("_T")


def _reject_nonfinite(value: str) -> object:
    raise ValueError("Xero returned a non-finite JSON number")


def _exact_integer(value: str) -> int | ExactDecimal:
    return ExactDecimal(value) if value == "-0" else int(value)


def _field(value: object, name: str, expected: type, default: Any) -> Any:
    if not isinstance(value, dict):
        raise ValueError("response is not a JSON object")
    found = value.get(name)
    if found is None:
        return default
    if expected is int and type(found) is ExactDecimal and str(found) == "-0":
        return 0
    if expected is bool:
        if type(found) is bool:
            return found
        if isinstance(found, str) and found.lower() in ("true", "false"):
            return found.lower() == "true"
        raise ValueError(f"{name} is not a boolean")
    if type(found) is not expected or (
        expected is int and isinstance(found, int) and not -(2**63) <= found <= 2**63 - 1
    ):
        raise ValueError(f"{name} has an invalid type")
    return found


def _decimal(value: object, name: str) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        value = value.strip()
        if not value:
            return ""
    if type(value) not in (str, int, ExactDecimal) or not _DECIMAL.fullmatch(str(value)):
        raise ValueError(f"{name} is not an exact decimal")
    return str(value)


def _wire_decimal(value: object, name: str) -> str:
    if not isinstance(value, dict):
        raise ValueError("response is not a JSON object")
    return _decimal(value.get(name), name)


def _date(value: str, required: bool = True) -> str:
    if not value and not required:
        return ""
    if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", value):
        raise ValueError("invalid calendar date")
    datetime.strptime(value, "%Y-%m-%d")
    return value


def _dotnet(value: str) -> datetime:
    if not value:
        return _ZERO_TIME
    match = _DOTNET.fullmatch(value)
    if match is None:
        raise ValueError("invalid Xero date")
    seconds, milliseconds = divmod(int(match.group(1)), 1000)
    return datetime.fromtimestamp(seconds, UTC) + timedelta(milliseconds=milliseconds)


def _wire_date(value: dict[str, Any], string_field: str, dotnet_field: str) -> str:
    string_value = _field(value, string_field, str, "")
    if string_value:
        return _date(string_value[:10]) if len(string_value) >= 10 else _date(string_value)
    dotnet_value = _field(value, dotnet_field, str, "")
    return _dotnet(dotnet_value).strftime("%Y-%m-%d") if dotnet_value else ""


def _contact(value: object) -> Contact:
    if not isinstance(value, dict):
        raise ValueError("contact is not an object")
    contact_id = _field(value, "ContactID", str, "")
    if not is_uuid(contact_id):
        raise ValueError("contact has no ContactID")
    return Contact(
        contact_id=contact_id.lower(),
        name=_field(value, "Name", str, ""),
        first_name=_field(value, "FirstName", str, ""),
        last_name=_field(value, "LastName", str, ""),
        email_address=_field(value, "EmailAddress", str, ""),
        contact_number=_field(value, "ContactNumber", str, ""),
        account_number=_field(value, "AccountNumber", str, ""),
        status=_field(value, "ContactStatus", str, ""),
        is_customer=_field(value, "IsCustomer", bool, False),
        is_supplier=_field(value, "IsSupplier", bool, False),
        default_currency=_field(value, "DefaultCurrency", str, ""),
        updated_at=_dotnet(_field(value, "UpdatedDateUTC", str, "")),
    )


def _page(
    body: bytes, collection: str, decoder: Callable[[object], _T], page: int, page_size: int
) -> tuple[tuple[_T, ...], int, int, bool]:
    document = json.loads(
        body, parse_int=_exact_integer, parse_float=ExactDecimal, parse_constant=_reject_nonfinite
    )
    values = _field(document, collection, list, None)
    if values is None:
        raise ValueError(f"response has no {collection} list")
    elements = tuple(decoder(value) for value in values)
    pagination = _field(document, "pagination", dict, None)
    page_count = _field(pagination, "pageCount", int, 0) if pagination else 0
    item_count = _field(pagination, "itemCount", int, 0) if pagination else 0
    has_more = page < page_count if page_count > 0 else len(elements) >= page_size
    return elements, page_count, item_count, has_more


def _single(body: bytes, collection: str) -> dict[str, Any]:
    document = json.loads(
        body, parse_int=_exact_integer, parse_float=ExactDecimal, parse_constant=_reject_nonfinite
    )
    values = _field(document, collection, list, None)
    if values is None or len(values) != 1 or not isinstance(values[0], dict):
        raise ValueError(f"response has no single {collection} element")
    return values[0]


def _invoice(value: object) -> Invoice:
    if not isinstance(value, dict):
        raise ValueError("invoice is not an object")
    invoice_id = _field(value, "InvoiceID", str, "")
    if not is_uuid(invoice_id):
        raise ValueError("invoice has no InvoiceID")
    invoice_type = _field(value, "Type", str, "")
    status = _field(value, "Status", str, "")
    if not invoice_type or not status:
        raise ValueError("invoice has no Type or Status")
    contact = _field(value, "Contact", dict, {})
    line_items = _field(value, "LineItems", list, [])
    payments = _field(value, "Payments", list, [])
    decoded_lines = tuple(
        InvoiceLineItem(
            line_item_id=_field(line, "LineItemID", str, ""),
            description=_field(line, "Description", str, ""),
            quantity=_wire_decimal(line, "Quantity"),
            unit_amount=_wire_decimal(line, "UnitAmount"),
            item_code=_field(line, "ItemCode", str, ""),
            account_code=_field(line, "AccountCode", str, ""),
            tax_type=_field(line, "TaxType", str, ""),
            tax_amount=_wire_decimal(line, "TaxAmount"),
            line_amount=_wire_decimal(line, "LineAmount"),
            discount_rate=_wire_decimal(line, "DiscountRate"),
        )
        for line in line_items
    )
    decoded_payments = []
    for payment in payments:
        payment_id = _field(payment, "PaymentID", str, "")
        if not is_uuid(payment_id):
            raise ValueError("invoice payment is invalid")
        decoded_payments.append(
            InvoicePayment(
                payment_id.lower(),
                _wire_date(payment, "DateString", "Date"),
                _wire_decimal(payment, "Amount"),
            )
        )
    return Invoice(
        invoice_id=invoice_id.lower(),
        type=invoice_type,
        status=status,
        invoice_number=_field(value, "InvoiceNumber", str, ""),
        contact=InvoiceContact(
            _field(contact, "ContactID", str, "").lower(), _field(contact, "Name", str, "")
        ),
        date=_wire_date(value, "DateString", "Date"),
        due_date=_wire_date(value, "DueDateString", "DueDate"),
        reference=_field(value, "Reference", str, ""),
        currency_code=_field(value, "CurrencyCode", str, ""),
        currency_rate=_wire_decimal(value, "CurrencyRate"),
        line_amount_types=_field(value, "LineAmountTypes", str, "") or None,
        line_items=decoded_lines,
        sub_total=_wire_decimal(value, "SubTotal"),
        total_tax=_wire_decimal(value, "TotalTax"),
        total=_wire_decimal(value, "Total"),
        amount_due=_wire_decimal(value, "AmountDue"),
        amount_paid=_wire_decimal(value, "AmountPaid"),
        amount_credited=_wire_decimal(value, "AmountCredited"),
        payments=tuple(decoded_payments),
        fully_paid_on_date=_wire_date(value, "", "FullyPaidOnDate"),
        is_sent_to_contact=_field(value, "SentToContact", bool, False),
        updated_at=_dotnet(_field(value, "UpdatedDateUTC", str, "")),
    )


def _payment(value: object) -> Payment:
    if not isinstance(value, dict):
        raise ValueError("payment is not an object")
    payment_id = _field(value, "PaymentID", str, "")
    if not is_uuid(payment_id) or value.get("Amount") is None:
        raise ValueError("payment has no PaymentID or Amount")
    invoice = _field(value, "Invoice", dict, {})
    account = _field(value, "Account", dict, {})
    return Payment(
        payment_id=payment_id.lower(),
        date=_wire_date(value, "DateString", "Date"),
        amount=_wire_decimal(value, "Amount"),
        bank_amount=_wire_decimal(value, "BankAmount"),
        currency_rate=_wire_decimal(value, "CurrencyRate"),
        reference=_field(value, "Reference", str, ""),
        status=_field(value, "Status", str, ""),
        payment_type=_field(value, "PaymentType", str, ""),
        is_reconciled=_field(value, "IsReconciled", bool, False),
        invoice=PaymentInvoice(
            invoice_id=_field(invoice, "InvoiceID", str, "").lower(),
            invoice_number=_field(invoice, "InvoiceNumber", str, ""),
            type=_field(invoice, "Type", str, "") or None,
            status=_field(invoice, "Status", str, "") or None,
            amount_due=_wire_decimal(invoice, "AmountDue"),
            amount_paid=_wire_decimal(invoice, "AmountPaid"),
            currency_code=_field(invoice, "CurrencyCode", str, ""),
        ),
        account=PaymentAccount(
            _field(account, "AccountID", str, "").lower(), _field(account, "Code", str, "")
        ),
        updated_at=_dotnet(_field(value, "UpdatedDateUTC", str, "")),
    )


def _page_bounds(page: int, page_size: int) -> tuple[int, int]:
    if page < 0 or page_size < 0 or page_size > 100:
        raise ValueError("page and pageSize must be positive and pageSize at most 100")
    return page or 1, page_size or 25


def _ids(values: tuple[str, ...], name: str) -> None:
    if len(values) > 40 or any(not is_uuid(value) for value in values):
        raise ValueError(f"{name} must contain at most 40 Xero UUIDs")


def _single_line(value: str, name: str, limit: int) -> str:
    trimmed = value.strip()
    if len(trimmed) > limit or any(ord(ch) < 32 or ord(ch) == 127 for ch in trimmed):
        raise ValueError(f"{name} must be one line of at most {limit} characters")
    return trimmed


def _valid_bare_email(value: str) -> bool:
    try:
        if not value or len(value.encode("utf-8")) > 254 or value.count("@") != 1:
            return False
    except UnicodeEncodeError:
        return False
    if any(ch in value for ch in " \"\\'()<>,;"):
        return False
    local, domain = value.split("@")
    if not local or not domain or local.startswith(".") or local.endswith(".") or ".." in local:
        return False
    if domain.startswith("[") and domain.endswith("]"):
        try:
            return isinstance(ipaddress.ip_address(domain[1:-1]), ipaddress.IPv4Address)
        except ValueError:
            return False
    if domain.startswith(".") or domain.endswith(".") or ".." in domain:
        return False
    return parseaddr(value) == ("", value)


def _invoice_number(value: str) -> str:
    if len(value.encode("utf-8")) > 255 or any(ord(ch) < 32 or ord(ch) > 126 or ch in '"\\' for ch in value):
        raise ValueError("invoiceNumber must be printable ASCII without double quotes or backslashes")
    return value


def _validate_decimal(
    value: str, name: str, fraction: int, integer: int, allow_negative: bool = False, allow_zero: bool = False
) -> str:
    if not _DECIMAL.fullmatch(value):
        raise ValueError(
            f"{name} must be an exact decimal number such as 1800.00, without an exponent or leading zeros"
        )
    whole, _, fractional = value.lstrip("-").partition(".")
    if (
        len(fractional) > fraction
        or len(whole) > integer
        or (ExactDecimal(value) < 0 and not allow_negative)
        or (ExactDecimal(value) == 0 and not allow_zero)
    ):
        raise ValueError(f"{name} is outside Xero's decimal range")
    return value


def _query_outcome(
    client: Client, call: Call, operation: str, result: Exchange, empty: _T, *, has_not_found: bool = False
) -> QueryAttempt[_T] | None:
    if result.outcome == "success":
        return None
    if result.outcome in ("retry", "server"):
        return QueryAttempt.retry(_require_failure(result), result.retry_after)
    branch = {
        "not_found": "notFound" if has_not_found else "providerRejected",
        "daily": "dailyLimitReached",
        "invalid": "invalidResponse",
        "defect": "defect",
    }.get(result.outcome, "providerRejected")
    return QueryAttempt.branch_result(branch, empty, client.receipt(call, result), result.error)


def _mutation_outcome(client: Client, call: Call, result: Exchange, empty: _T) -> MutationAttempt[_T] | None:
    if result.outcome == "success":
        return None
    if result.outcome == "retry":
        return MutationAttempt.retry(_require_failure(result), result.retry_after)
    if result.outcome in ("server", "invalid"):
        return MutationAttempt.uncertain(empty, _require_failure(result), client.receipt(call, result))
    branch = {"daily": "dailyLimitReached", "defect": "defect"}.get(result.outcome, "providerRejected")
    return MutationAttempt.branch_result(branch, empty, client.receipt(call, result), result.error)


def _require_failure(result: Exchange) -> Failure:
    if result.error is None:
        raise ValueError("classified Xero exchange has no failure")
    return result.error


class ListContactsOperation:
    """Read a bounded page of Xero contacts."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> QueryDefinition:
        return LIST_CONTACTS_DEFINITION

    async def invoke(self, call: Call, input: ListContactsInput) -> QueryAttempt[ContactPage]:
        operation = "listContacts"
        try:
            page, size = _page_bounds(input.page, input.page_size)
            search = _single_line(input.search_term, "searchTerm", 100)
            _ids(input.contact_ids, "contactIds")
            query = {"page": str(page), "pageSize": str(size)}
            if search:
                query["searchTerm"] = search
            if input.contact_ids:
                query["IDs"] = ",".join(input.contact_ids)
            if input.includes_archived:
                query["includeArchived"] = "true"
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", ContactPage(), Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        result = await self._client.exchange(
            call, operation, "GET", "/Contacts", query, modified_since=input.modified_since
        )
        outcome = _query_outcome(self._client, call, operation, result, ContactPage())
        if outcome is not None:
            return outcome
        try:
            contacts, count, items, more = _page(result.body, "Contacts", _contact, page, size)
            value = ContactPage(contacts, page, size, count, items, more)
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                ContactPage(),
                self._client.receipt(call, result),
                failure(operation, FailureKind.PROTOCOL, "Xero returned an invalid contact page"),
            )
        return QueryAttempt.branch_result("listed", value, self._client.receipt(call, result))


class FindContactByEmailOperation:
    """Resolve one contact by exact email address."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> QueryDefinition:
        return FIND_CONTACT_BY_EMAIL_DEFINITION

    async def invoke(
        self, call: Call, input: FindContactByEmailInput
    ) -> QueryAttempt[FindContactByEmailOutput]:
        operation = "findContactByEmail"
        email = input.email_address.strip()
        requested = FindContactByEmailOutput(email)
        if not _valid_bare_email(email):
            return QueryAttempt.branch_result(
                "defect",
                requested,
                Receipt(),
                failure(
                    operation,
                    FailureKind.VALIDATION,
                    "emailAddress must be one bare email address such as jane@example.com",
                ),
            )
        query = {"where": f'EmailAddress=="{email}"', "page": "1", "pageSize": "10"}
        if input.includes_archived:
            query["includeArchived"] = "true"
        result = await self._client.exchange(call, operation, "GET", "/Contacts", query)
        outcome = _query_outcome(self._client, call, operation, result, requested)
        if outcome is not None:
            return outcome
        try:
            contacts, _, _, _ = _page(result.body, "Contacts", _contact, 1, 10)
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                requested,
                self._client.receipt(call, result),
                failure(operation, FailureKind.PROTOCOL, "Xero returned an invalid contact list"),
            )
        matches = tuple(
            contact
            for contact in contacts
            if equal_fold(contact.email_address.strip(), email)
            and (input.includes_archived or contact.status in ("ACTIVE", ""))
        )
        if not matches:
            return QueryAttempt.branch_result(
                "notFound",
                requested,
                self._client.receipt(call, result),
                failure(operation, FailureKind.NOT_FOUND, "no Xero contact has the email address"),
            )
        if len(matches) > 1:
            return QueryAttempt.branch_result(
                "ambiguous",
                FindContactByEmailOutput(email, matches=matches),
                self._client.receipt(call, result),
                failure(operation, FailureKind.CONFLICT, "several Xero contacts have the email address"),
            )
        return QueryAttempt.branch_result(
            "found",
            FindContactByEmailOutput(email, contact=matches[0]),
            self._client.receipt(call, result, matches[0].contact_id),
        )


class GetInvoiceOperation:
    """Read one Xero invoice by UUID or invoice number."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> QueryDefinition:
        return GET_INVOICE_DEFINITION

    async def invoke(self, call: Call, input: GetInvoiceInput) -> QueryAttempt[Invoice]:
        operation = "getInvoice"
        invoice_id, number = input.invoice_id.strip(), input.invoice_number.strip()
        if (
            bool(invoice_id) == bool(number)
            or invoice_id
            and not is_uuid(invoice_id)
            or number
            and (len(number) > 255 or any(ord(ch) < 32 or ord(ch) > 126 or ch in '"\\' for ch in number))
        ):
            return QueryAttempt.branch_result(
                "defect",
                Invoice(type="", status=""),
                Receipt(),
                failure(operation, FailureKind.VALIDATION, "invoiceId or invoiceNumber is invalid"),
            )
        identifier = invoice_id.lower() or number
        result = await self._client.exchange(
            call, operation, "GET", "/Invoices/" + go_path_escape(identifier), {"unitdp": "4"}
        )
        empty = Invoice(type="", status="")
        outcome = _query_outcome(self._client, call, operation, result, empty, has_not_found=True)
        if outcome is not None:
            return outcome
        try:
            wire = _single(result.body, "Invoices")
            if (
                _field(wire, "InvoiceID", str, "").lower() != identifier.lower()
                and _field(wire, "InvoiceNumber", str, "") != identifier
            ):
                raise ValueError("response describes another invoice")
            invoice = _invoice(wire)
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self._client.receipt(call, result),
                failure(operation, FailureKind.PROTOCOL, "Xero returned an invalid invoice"),
            )
        return QueryAttempt.branch_result(
            "found", invoice, self._client.receipt(call, result, invoice.invoice_id)
        )


class ListInvoicesOperation:
    """Read one filtered, bounded invoice page."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> QueryDefinition:
        return LIST_INVOICES_DEFINITION

    async def invoke(self, call: Call, input: ListInvoicesInput) -> QueryAttempt[InvoicePage]:
        operation = "listInvoices"
        try:
            page, size = _page_bounds(input.page, input.page_size)
            if any(
                status not in ("DRAFT", "SUBMITTED", "DELETED", "AUTHORISED", "PAID", "VOIDED")
                for status in input.statuses
            ):
                raise ValueError("statuses contains an invalid Xero status")
            _ids(input.contact_ids, "contactIds")
            if len(input.invoice_numbers) > 40 or any(
                not value or "," in value or _invoice_number(value) != value
                for value in input.invoice_numbers
            ):
                raise ValueError("invoiceNumbers contains an invalid number")
            if input.type and input.type not in ("ACCREC", "ACCPAY"):
                raise ValueError("type must be ACCREC or ACCPAY")
            reference = _single_line(input.reference, "reference", 255)
            if any(ch in reference for ch in '"\\'):
                raise ValueError("reference is invalid")
            query = {"page": str(page), "pageSize": str(size), "unitdp": "4"}
            if input.statuses:
                query["Statuses"] = ",".join(input.statuses)
            if input.contact_ids:
                query["ContactIDs"] = ",".join(input.contact_ids)
            if input.invoice_numbers:
                query["InvoiceNumbers"] = ",".join(input.invoice_numbers)
            where = []
            if input.type:
                where.append(f'Type=="{input.type}"')
            if reference:
                where.append(f'Reference=="{reference}"')
            if where:
                query["where"] = " AND ".join(where)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", InvoicePage(), Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        result = await self._client.exchange(
            call, operation, "GET", "/Invoices", query, modified_since=input.modified_since
        )
        outcome = _query_outcome(self._client, call, operation, result, InvoicePage())
        if outcome is not None:
            return outcome
        try:
            invoices, count, items, more = _page(result.body, "Invoices", _invoice, page, size)
            value = InvoicePage(invoices, page, size, count, items, more)
        except (ValueError, TypeError, KeyError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                InvoicePage(),
                self._client.receipt(call, result),
                failure(operation, FailureKind.PROTOCOL, "Xero returned an invalid invoice page"),
            )
        return QueryAttempt.branch_result("listed", value, self._client.receipt(call, result))


class CreateInvoiceOperation:
    """Create one invoice under the Call ID idempotency key."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> MutationDefinition:
        return CREATE_INVOICE_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: CreateInvoiceInput) -> IdempotencyKey:
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreateInvoiceInput) -> MutationAttempt[CreateInvoiceOutput]:
        operation = "createInvoice"
        requested = CreateInvoiceOutput(Invoice(type="", status=""), input.reference.strip())
        try:
            if input.type not in ("ACCREC", "ACCPAY") or not is_uuid(input.contact_id.strip()):
                raise ValueError("type or contactId is invalid")
            for value in (input.date, input.due_date):
                if value:
                    _date(value.strip())
            if input.status not in (None, "", "DRAFT", "SUBMITTED", "AUTHORISED"):
                raise ValueError("status of a new invoice is invalid")
            if input.line_amount_types not in (None, "", "Exclusive", "Inclusive", "NoTax"):
                raise ValueError("lineAmountTypes is invalid")
            if input.currency_code and not re.fullmatch(r"[A-Z]{3}", input.currency_code.strip()):
                raise ValueError("currencyCode is invalid")
            _invoice_number(input.invoice_number.strip())
            _single_line(input.reference, "reference", 255)
            if input.branding_theme_id and not is_uuid(input.branding_theme_id.strip()):
                raise ValueError("brandingThemeId is invalid")
            if not 1 <= len(input.line_items) <= 100:
                raise ValueError("lineItems must hold 1 to 100 lines")
            lines: list[dict[str, object]] = []
            for item in input.line_items:
                if isinstance(item, dict):
                    item = InvoiceLineItemInput(
                        **{
                            "description": item.get("description", ""),
                            "quantity": item.get("quantity", ""),
                            "unit_amount": item.get("unitAmount", ""),
                            "account_code": item.get("accountCode", ""),
                            "item_code": item.get("itemCode", ""),
                            "tax_type": item.get("taxType", ""),
                            "discount_rate": item.get("discountRate", ""),
                        }
                    )
                if not item.description.strip():
                    raise ValueError("line description is required")
                if len(item.description.strip()) > 4000 or any(
                    0xD800 <= ord(ch) <= 0xDFFF for ch in item.description
                ):
                    raise ValueError("line description is invalid")
                line: dict[str, object] = {"Description": item.description.strip()}
                for name, value in (
                    ("AccountCode", item.account_code),
                    ("ItemCode", item.item_code),
                    ("TaxType", item.tax_type),
                ):
                    if value.strip():
                        if not _CODE.fullmatch(value.strip()):
                            raise ValueError("line code is invalid")
                        line[name] = value.strip()
                for name, value, places, digits, negative, zero in (
                    ("Quantity", item.quantity, 4, 12, True, True),
                    ("UnitAmount", item.unit_amount, 4, 12, True, True),
                    ("DiscountRate", item.discount_rate, 4, 3, False, True),
                ):
                    if value:
                        _validate_decimal(value, name, places, digits, negative, zero)
                        line[name] = ExactDecimal(value)
                if item.discount_rate and (input.type != "ACCREC" or ExactDecimal(item.discount_rate) > 100):
                    raise ValueError("discountRate is invalid")
                lines.append(line)
            payload = {
                "Type": input.type,
                "Contact": {"ContactID": input.contact_id.strip().lower()},
                "LineItems": lines,
            }
            for name, value in (
                ("Date", input.date),
                ("DueDate", input.due_date),
                ("CurrencyCode", input.currency_code),
                ("InvoiceNumber", input.invoice_number),
                ("Reference", input.reference),
                ("Status", input.status or ""),
                ("LineAmountTypes", input.line_amount_types or ""),
                ("BrandingThemeID", input.branding_theme_id),
            ):
                if value and value.strip():
                    payload[name] = value.strip().lower() if name == "BrandingThemeID" else value.strip()
            body = _json_decimal(payload)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        result = await self._client.exchange(
            call, operation, "PUT", "/Invoices", {"unitdp": "4"}, body, mutation=True
        )
        outcome = _mutation_outcome(self._client, call, result, requested)
        if outcome is not None:
            return outcome
        try:
            wire = _single(result.body, "Invoices")
            if _field(wire, "HasErrors", bool, False) or _field(wire, "ValidationErrors", list, []):
                return MutationAttempt.branch_result(
                    "providerRejected",
                    requested,
                    self._client.receipt(call, result),
                    failure(operation, FailureKind.VALIDATION, "Xero rejected the invoice"),
                )
            invoice = _invoice(wire)
            warnings = len(_field(wire, "Warnings", list, []))
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.uncertain(
                requested,
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Xero accepted the invoice but returned an unusable invoice",
                ),
                self._client.receipt(call, result),
            )
        return MutationAttempt.branch_result(
            "created",
            CreateInvoiceOutput(invoice, requested.reference, warnings),
            self._client.receipt(call, result, invoice.invoice_id),
        )


class RecordPaymentOperation:
    """Record one payment under the Call ID idempotency key."""

    def __init__(self, client: Client) -> None:
        self._client = client

    def definition(self) -> MutationDefinition:
        return RECORD_PAYMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: RecordPaymentInput) -> IdempotencyKey:
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: RecordPaymentInput) -> MutationAttempt[RecordPaymentOutput]:
        operation = "recordPayment"
        requested = RecordPaymentOutput(Payment(status=""), input.invoice_id.strip().lower())
        try:
            if not is_uuid(input.invoice_id.strip()):
                raise ValueError("invoiceId is invalid")
            if bool(input.account_id.strip()) == bool(input.account_code.strip()):
                raise ValueError("set exactly one of accountId and accountCode")
            if input.account_id and not is_uuid(input.account_id.strip()):
                raise ValueError("accountId is invalid")
            if input.account_code and not _CODE.fullmatch(input.account_code.strip()):
                raise ValueError("accountCode is invalid")
            _date(input.date.strip())
            _validate_decimal(input.amount, "amount", 2, 12)
            if input.bank_amount:
                _validate_decimal(input.bank_amount, "bankAmount", 2, 12)
            reference = _single_line(input.reference, "reference", 255)
            payload = {
                "Invoice": {"InvoiceID": input.invoice_id.strip().lower()},
                "Account": {"AccountID": input.account_id.strip().lower()}
                if input.account_id
                else {"Code": input.account_code.strip()},
                "Date": input.date.strip(),
                "Amount": ExactDecimal(input.amount),
            }
            if input.bank_amount:
                payload["BankAmount"] = ExactDecimal(input.bank_amount)
            if reference:
                payload["Reference"] = reference
            if input.is_reconciled:
                payload["IsReconciled"] = True
            body = _json_decimal(payload)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", requested, Receipt(), failure(operation, FailureKind.VALIDATION, str(error))
            )
        result = await self._client.exchange(call, operation, "PUT", "/Payments", payload=body, mutation=True)
        outcome = _mutation_outcome(self._client, call, result, requested)
        if outcome is not None:
            return outcome
        try:
            wire = _single(result.body, "Payments")
            if _field(wire, "HasValidationErrors", bool, False) or _field(wire, "ValidationErrors", list, []):
                return MutationAttempt.branch_result(
                    "providerRejected",
                    requested,
                    self._client.receipt(call, result),
                    failure(operation, FailureKind.VALIDATION, "Xero rejected the payment"),
                )
            payment = _payment(wire)
            if payment.invoice.invoice_id and payment.invoice.invoice_id != requested.invoice_id:
                raise ValueError("payment applies to another invoice")
            warnings = len(_field(wire, "Warnings", list, []))
        except (ValueError, TypeError, KeyError):
            return MutationAttempt.uncertain(
                requested,
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Xero accepted the payment but returned an unusable payment",
                ),
                self._client.receipt(call, result),
            )
        return MutationAttempt.branch_result(
            "recorded",
            RecordPaymentOutput(payment, requested.invoice_id, warnings),
            self._client.receipt(call, result, payment.payment_id),
        )


def _json_decimal(value: object) -> bytes:
    if isinstance(value, ExactDecimal):
        return str(value).encode()
    if isinstance(value, dict):
        return (
            b"{"
            + b",".join(
                json.dumps(key, ensure_ascii=False).encode() + b":" + _json_decimal(item)
                for key, item in value.items()
            )
            + b"}"
        )
    if isinstance(value, list):
        return b"[" + b",".join(_json_decimal(item) for item in value) + b"]"
    return json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode()
