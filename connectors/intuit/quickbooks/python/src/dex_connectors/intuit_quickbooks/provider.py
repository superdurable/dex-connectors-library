# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""QuickBooks wire decoding and request validation shared by operation invokes."""

import json
import re
from datetime import UTC, date, datetime, timedelta
from decimal import Decimal, InvalidOperation
from email.headerregistry import Address
from email.parser import Parser
from email.policy import default
from typing import Any

from .models import Customer, Invoice, InvoiceLine, Payment, PaymentApplication

_ID = re.compile(r"[0-9]{1,20}\Z")
_DECIMAL = re.compile(r"-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,10})?\Z")
_WIRE_DECIMAL = re.compile(r"-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,18})?(?:[eE][+-]?[0-9]{1,2})?\Z")


def object_value(value: Any) -> dict[str, Any]:
    """Require a JSON object without coercing provider values."""
    if not isinstance(value, dict):
        raise ValueError("response is not a JSON object")
    return value


def string_field(value: dict[str, Any], name: str) -> str:
    """Read a Go string field, accepting absence but rejecting a wrong type."""
    item = value.get(name, "")
    if item is None:
        return ""
    if not isinstance(item, str):
        raise ValueError(f"response {name} is malformed")
    return item


def object_field(value: dict[str, Any], name: str) -> dict[str, Any] | None:
    """Read an optional Go pointer-to-struct field."""
    item = value.get(name)
    return None if item is None else object_value(item)


def list_field(value: dict[str, Any], name: str) -> list[Any]:
    """Read a Go slice field."""
    item = value.get(name)
    if item is None:
        return []
    if not isinstance(item, list):
        raise ValueError(f"response {name} is not a list")
    return item


def validate_id(name: str, value: str) -> str:
    """Require a QuickBooks decimal entity ID."""
    if not _ID.fullmatch(value):
        raise ValueError(f"{name} must be a QuickBooks Id, a decimal string such as 145")
    return value


def validate_date(name: str, value: str) -> str:
    """Reject impossible calendar dates as Go time.Parse does."""
    if value:
        if not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}", value):
            raise ValueError(f"{name} must be a calendar date written YYYY-MM-DD, such as 2026-10-01")
        try:
            date.fromisoformat(value)
        except ValueError:
            raise ValueError(
                f"{name} must be a calendar date written YYYY-MM-DD, such as 2026-10-01"
            ) from None
    return value


def validate_text(name: str, value: str, maximum: int, multiline: bool = False) -> str:
    """Apply QuickBooks text bounds and control-character rules."""
    if len(value) > maximum:
        raise ValueError(f"{name} can be at most {maximum} characters")
    if any(
        (ord(character) < 32 or ord(character) == 127) and not (multiline and character in "\n\r\t")
        for character in value
    ):
        raise ValueError(f"{name} must not contain control characters")
    return value


def validate_email(name: str, value: str) -> str:
    """Require one bare RFC 5322 address, mirroring Go mail.ParseAddress."""
    if not value:
        raise ValueError(f"{name} is required")
    if len(value.encode()) > 254:
        raise ValueError(f"{name} is longer than 254 bytes")
    # The policy parser accepts quoted and named addresses; the Go connector rejects both.
    parsed = Parser(policy=default).parsestr("To: " + value + "\n\n")["To"]
    addresses = list(getattr(parsed, "addresses", ()))
    if (
        "@" not in value
        or len(addresses) != 1
        or not isinstance(addresses[0], Address)
        or str(addresses[0]) != value
        or any(character in value for character in " \"\\'()<>,;")
    ):
        raise ValueError(f"{name} must be one bare email address such as jane@example.com")
    return value


def validate_currency(name: str, value: str) -> str:
    """Require an uppercase ISO currency code when set."""
    if value and not re.fullmatch(r"[A-Z]{3}", value):
        raise ValueError(f"{name} must be a three-letter ISO 4217 code such as USD")
    return value


def query_literal(name: str, value: str, maximum: int) -> str:
    """Quote a QuickBooks query literal after validating it."""
    validate_text(name, value, maximum)
    if "\\" in value:
        raise ValueError(f"{name} cannot contain a backslash")
    return "'" + value.replace("'", "\\'") + "'"


def validate_decimal(
    name: str,
    value: str,
    integer_digits: int,
    fraction_digits: int,
    positive: bool = False,
    allow_negative: bool = False,
) -> str:
    """Validate an exact decimal before JSON number encoding."""
    if not _DECIMAL.fullmatch(value):
        raise ValueError(
            f"{name} must be an exact decimal number such as 1800.00, without an exponent or leading zeros"
        )
    integer, _, fraction = value.lstrip("-").partition(".")
    if len(fraction) > fraction_digits:
        raise ValueError(f"{name} can have at most {fraction_digits} decimal places")
    if len(integer) > integer_digits:
        raise ValueError(f"{name} can have at most {integer_digits} digits before the decimal point")
    if value.startswith("-") and Decimal(value) != 0 and not allow_negative:
        raise ValueError(f"{name} cannot be negative")
    if positive and Decimal(value) == 0:
        raise ValueError(f"{name} must be greater than zero")
    return value


def wire_decimal(value: Any, required: bool = False) -> str:
    """Keep exact provider digits and expand JSON exponent notation."""
    if value is None or value == "":
        if required:
            raise ValueError("response has no required amount")
        return ""
    if isinstance(value, bool) or not isinstance(value, (str, int, float, Decimal)):
        raise ValueError("response amount is malformed")
    text = str(value)
    if not _WIRE_DECIMAL.fullmatch(text):
        raise ValueError("response amount is malformed")
    try:
        plain = format(Decimal(text), "f")
    except InvalidOperation:
        raise ValueError("response amount is malformed") from None
    if "." in text and "e" not in text.lower():
        plain = text
    if not _DECIMAL.fullmatch(plain):
        raise ValueError("response amount is malformed")
    return plain.lstrip("-") if Decimal(plain) == 0 and text.startswith("-") else plain


def parse_timestamp(value: str) -> datetime | None:
    """Decode Go-compatible RFC 3339 timestamps, including large offsets."""
    if not value:
        return None
    match = re.fullmatch(
        r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})"
        r"(?:[.,]([0-9]+))?(Z|[+-][0-9]{2}:[0-9]{2})",
        value,
    )
    if match is None:
        raise ValueError("invalid provider timestamp")
    year, month, day, hour, minute, second, fraction, offset = match.groups()
    try:
        parsed = datetime(
            int(year),
            int(month),
            int(day),
            int(hour),
            int(minute),
            int(second),
            int((fraction or "")[:6].ljust(6, "0")),
            tzinfo=UTC,
        )
        if offset != "Z":
            offset_hours, offset_minutes = map(int, offset[1:].split(":"))
            if offset_minutes > 59:
                raise ValueError("invalid offset minutes")
            duration = timedelta(hours=offset_hours, minutes=offset_minutes)
            parsed += -duration if offset[0] == "+" else duration
        return parsed
    except (ValueError, OverflowError):
        raise ValueError("invalid provider timestamp") from None


def metadata_times(value: dict[str, Any]) -> tuple[datetime | None, datetime | None]:
    """Read optional QuickBooks creation and update times."""
    metadata = object_field(value, "MetaData")
    if metadata is None:
        return None, None
    return parse_timestamp(string_field(metadata, "CreateTime")), parse_timestamp(
        string_field(metadata, "LastUpdatedTime")
    )


def entity(body: bytes, name: str) -> dict[str, Any]:
    """Decode one named QuickBooks entity envelope."""
    envelope = object_value(json.loads(body, parse_float=str, parse_int=str))
    if name not in envelope:
        raise ValueError(f"response has no {name}")
    return object_value(envelope[name])


def query_entities(body: bytes, name: str) -> list[dict[str, Any]]:
    """Decode a QuickBooks QueryResponse list."""
    envelope = object_value(json.loads(body, parse_float=str, parse_int=str))
    query = object_field(envelope, "QueryResponse")
    if query is None:
        raise ValueError("response has no QueryResponse")
    return [object_value(item) for item in list_field(query, name)]


def decode_customer(value: dict[str, Any]) -> Customer:
    """Validate and map a QuickBooks Customer."""
    identifier = validate_id("customer Id", string_field(value, "Id"))
    name = string_field(value, "DisplayName")
    if not name.strip():
        raise ValueError("customer has no DisplayName")
    email = object_field(value, "PrimaryEmailAddr")
    phone = object_field(value, "PrimaryPhone")
    currency = object_field(value, "CurrencyRef")
    active = value.get("Active")
    if active is not None and type(active) is not bool:
        raise ValueError("customer Active is malformed")
    created, updated = metadata_times(value)
    return Customer(
        identifier,
        string_field(value, "SyncToken"),
        name,
        string_field(value, "GivenName"),
        string_field(value, "FamilyName"),
        string_field(value, "CompanyName"),
        string_field(email, "Address").strip() if email else "",
        string_field(phone, "FreeFormNumber") if phone else "",
        active is not False,
        wire_decimal(value.get("Balance")),
        string_field(currency, "value") if currency else "",
        created,
        updated,
    )


def decode_invoice(value: dict[str, Any]) -> Invoice:
    """Validate and map a QuickBooks Invoice and its item lines."""
    identifier = validate_id("invoice Id", string_field(value, "Id"))
    customer = object_field(value, "CustomerRef")
    if customer is None:
        raise ValueError("invoice has no valid CustomerRef")
    customer_id = validate_id("CustomerRef", string_field(customer, "value"))
    total = wire_decimal(value.get("TotalAmt"), True)
    balance = wire_decimal(value.get("Balance"), True)
    txn_date = validate_date("TxnDate", string_field(value, "TxnDate"))
    due_date = validate_date("DueDate", string_field(value, "DueDate"))
    currency = object_field(value, "CurrencyRef")
    tax = object_field(value, "TxnTaxDetail")
    email = object_field(value, "BillEmail")
    delivery = object_field(value, "DeliveryInfo")
    memo = object_field(value, "CustomerMemo")
    lines = []
    for element in list_field(value, "Line"):
        line = object_value(element)
        detail_type = string_field(line, "DetailType")
        if not detail_type:
            raise ValueError("invoice line has no DetailType")
        detail = object_field(line, "SalesItemLineDetail")
        item = object_field(detail, "ItemRef") if detail else None
        tax_code = object_field(detail, "TaxCodeRef") if detail else None
        lines.append(
            InvoiceLine(
                line_id=string_field(line, "Id"),
                detail_type=detail_type,
                description=string_field(line, "Description"),
                amount=wire_decimal(line.get("Amount")),
                item_id=string_field(item, "value") if item else "",
                item_name=string_field(item, "name") if item else "",
                quantity=wire_decimal(detail.get("Qty")) if detail else "",
                unit_price=wire_decimal(detail.get("UnitPrice")) if detail else "",
                tax_code_id=string_field(tax_code, "value") if tax_code else "",
                service_date=string_field(detail, "ServiceDate") if detail else "",
            )
        )
    payments = []
    for linked in list_field(value, "LinkedTxn"):
        linked = object_value(linked)
        if string_field(linked, "TxnType") == "Payment" and _ID.fullmatch(string_field(linked, "TxnId")):
            payments.append(string_field(linked, "TxnId"))
    created, updated = metadata_times(value)
    return Invoice(
        invoice_id=identifier,
        sync_token=string_field(value, "SyncToken"),
        doc_number=string_field(value, "DocNumber"),
        customer_id=customer_id,
        customer_name=string_field(customer, "name"),
        txn_date=txn_date,
        due_date=due_date,
        currency_code=string_field(currency, "value") if currency else "",
        exchange_rate=wire_decimal(value.get("ExchangeRate")),
        lines=tuple(lines),
        total_amount=total,
        total_tax=wire_decimal(tax.get("TotalTax")) if tax else "",
        balance=balance,
        email_status=string_field(value, "EmailStatus") or None,
        bill_email=string_field(email, "Address").strip() if email else "",
        delivered_at=parse_timestamp(string_field(delivery, "DeliveryTime")) if delivery else None,
        customer_memo=string_field(memo, "value") if memo else "",
        private_note=string_field(value, "PrivateNote"),
        linked_payment_ids=tuple(payments),
        created_at=created,
        updated_at=updated,
    )


def decode_payment(value: dict[str, Any]) -> Payment:
    """Validate and map a QuickBooks Payment and its invoice applications."""
    identifier = validate_id("payment Id", string_field(value, "Id"))
    customer = object_field(value, "CustomerRef")
    if customer is None:
        raise ValueError("payment has no valid CustomerRef")
    customer_id = validate_id("CustomerRef", string_field(customer, "value"))
    total = wire_decimal(value.get("TotalAmt"), True)
    txn_date = validate_date("TxnDate", string_field(value, "TxnDate"))
    currency = object_field(value, "CurrencyRef")
    deposit = object_field(value, "DepositToAccountRef")
    applications = []
    for line_value in list_field(value, "Line"):
        line = object_value(line_value)
        for linked_value in list_field(line, "LinkedTxn"):
            linked = object_value(linked_value)
            if string_field(linked, "TxnType") == "Invoice":
                applications.append(
                    PaymentApplication(
                        validate_id("TxnId", string_field(linked, "TxnId")),
                        wire_decimal(line.get("Amount"), True),
                    )
                )
    created, updated = metadata_times(value)
    return Payment(
        payment_id=identifier,
        sync_token=string_field(value, "SyncToken"),
        customer_id=customer_id,
        txn_date=txn_date,
        total_amount=total,
        unapplied_amount=wire_decimal(value.get("UnappliedAmt")),
        currency_code=string_field(currency, "value") if currency else "",
        payment_reference_number=string_field(value, "PaymentRefNum"),
        deposit_account_id=string_field(deposit, "value") if deposit else "",
        applied_invoices=tuple(applications),
        private_note=string_field(value, "PrivateNote"),
        created_at=created,
        updated_at=updated,
    )
