# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Gmail HTTP calls, MIME construction, and Sent Mail read-back helpers."""

import base64
import binascii
import hashlib
import json
import math
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from email import policy
from email.header import decode_header, make_header
from email.headerregistry import Address
from email.parser import Parser
from email.quoprimime import header_encode
from email.utils import parseaddr
from urllib.parse import quote, urlencode

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    CredentialRefreshDriver,
    RejectedCredentialRefreshingProvider,
    TriggerLogger,
    resolve_credential,
    resolve_credential_after_rejection,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from .models import Config, Credentials, Message, ReplyToMessageInput, SendMessageInput
from .project_credentials import StoredCredentialMaterialError
from .refresh import CredentialRefreshDriver as GmailCredentialRefreshDriver


@dataclass(frozen=True, slots=True)
class GmailHTTPResult:
    """Safe bounded response metadata; the body is never logged."""

    # HTTP status returned by Gmail.
    status_code: int
    # HTTP headers for request ID and Retry-After classification.
    headers: httpx.Headers
    # Complete body bounded by Config.max_response_bytes.
    body: bytes


class GmailResponseTooLarge(BodyTooLargeError):
    """Bounded response overflow retaining only safe status and response headers."""

    def __init__(self, status_code: int, headers: httpx.Headers) -> None:
        super().__init__(status_code, headers)


class GmailResponseReadFailure(OSError):
    """Response stream failed after Gmail supplied safe status and headers."""

    def __init__(self, status_code: int, headers: httpx.Headers) -> None:
        self.status_code = status_code
        self.headers = headers.copy()
        super().__init__("Gmail response body could not be read")


class GmailCredentialMaterialError(ValueError):
    """Resolved credential material is invalid, so only reconnection can repair it."""


# Repeats the Message-ID the connector sets, because Gmail replaces Message-ID on send.
_DEX_MESSAGE_ID_HEADER = "X-Dex-Message-ID"
# Covers the Execute retry window of a send with a wide margin.
_SENT_MAIL_CANDIDATE_WINDOW = "newer_than:2d"
_SENT_MAIL_CANDIDATE_LIMIT = 25
# Admits only characters that Gmail search reads literally in a to: term.
_SEARCHABLE_RECIPIENT_ADDRESS = re.compile(r"[A-Za-z0-9._+-]+@[A-Za-z0-9.-]+")
_UNPADDED_BASE64URL = re.compile(r"[A-Za-z0-9_-]*")
_PADDED_BASE64URL = re.compile(r"[A-Za-z0-9_-]+={1,2}")
# Go's net/mail admits a tab but no other control character outside an encoded word.
_CONTROL_CHARACTER = re.compile(r"[\x00-\x08\x0a-\x1f\x7f]")
_MAILBOX_WITH_ANGLE_ADDRESS = re.compile(r"(.*?)<([^<>]+)>")
_PLAIN_MAILBOX_ADDRESS = re.compile(r"[^<>\s,;@]+@[^<>\s,;@]+")
_QUOTED_DISPLAY_NAME = re.compile(r'"(?:[^"\\]|\\.)*"', re.DOTALL)
_UNQUOTED_DISPLAY_NAME_SPECIAL = re.compile(r'[,:;<>@"\\()\[\]]')
_ENCODED_WORD = re.compile(r"=\?([^?]+)\?([BbQq])\?([^?]*)\?=")
_PADDED_BASE64 = re.compile(r"(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?")
_Q_ENCODED_BYTE = re.compile(r"[0-9A-Fa-f]{2}")
# Go's mail.Address.String keeps printable ASCII, space, and tab in a quoted string.
_QUOTABLE_DISPLAY_NAME = re.compile(r"[\t\x20-\x7e]*")
# RFC 2047 5(3) forbids these in a Q-encoded phrase, so Go B-encodes; Go misses the backslash.
_B_ENCODED_DISPLAY_NAME_CHARACTER = re.compile(r"[\"#$%&'(),.:;<>@\[\\\]^`{|}~]")
# An encoded word has at most 75 characters, 63 of them between "=?utf-8?q?" and "?=".
_MAX_ENCODED_WORD_TEXT_LENGTH = 63
# 45 bytes fill 60 Base64 characters, the most that fit in 63.
_MAX_B_ENCODED_WORD_BYTES = 45


def _base64url_decode(value: str) -> str:
    # Go and TypeScript reject the standard alphabet and malformed padding, so this does too.
    is_unpadded = _UNPADDED_BASE64URL.fullmatch(value) is not None and len(value) % 4 != 1
    is_padded = _PADDED_BASE64URL.fullmatch(value) is not None and len(value) % 4 == 0
    if not is_unpadded and not is_padded:
        raise ValueError("Gmail body is not base64url")
    return base64.urlsafe_b64decode(value + "=" * (-len(value) % 4)).decode("utf-8", errors="replace")


def decode_message(resource: object) -> Message:
    """Decode one Gmail full or metadata response, rejecting unusable identity and body."""
    if not isinstance(resource, dict) or not isinstance(resource.get("id"), str) or not resource["id"]:
        raise ValueError("Gmail message identity is missing")
    if not isinstance(resource.get("threadId"), str) or not resource["threadId"]:
        raise ValueError("Gmail thread identity is missing")
    payload = resource.get("payload")
    if payload is None:
        payload = {}
    if not isinstance(payload, dict):
        raise ValueError("Gmail payload is invalid")
    headers = payload.get("headers") or []
    if not isinstance(headers, list):
        raise ValueError("Gmail headers are invalid")
    header_values: dict[str, str] = {}
    for header in headers:
        if (
            not isinstance(header, dict)
            or not isinstance(header.get("name"), str)
            or not isinstance(header.get("value"), str)
        ):
            raise ValueError("Gmail header is invalid")
        header_values[header["name"].lower()] = header["value"]
    if not isinstance(resource.get("internalDate"), str) or not resource["internalDate"]:
        raise ValueError("Gmail message date is invalid")
    try:
        milliseconds = int(resource["internalDate"])
        received_at = datetime.fromtimestamp(milliseconds / 1000, UTC)
    except (KeyError, TypeError, ValueError, OverflowError) as error:
        raise ValueError("Gmail message date is invalid") from error
    text_bodies: list[str] = []
    html_bodies: list[str] = []

    def visit(part: dict[str, object]) -> None:
        body = part.get("body", {})
        if isinstance(body, dict) and body.get("data"):
            if not isinstance(body["data"], str):
                raise ValueError("Gmail message body is invalid")
            try:
                decoded = _base64url_decode(body["data"])
            except (ValueError, binascii.Error) as error:
                raise ValueError("Gmail message body cannot be decoded") from error
            mime_type_value = part.get("mimeType", "")
            if not isinstance(mime_type_value, str):
                raise ValueError("Gmail MIME type is invalid")
            mime_type = mime_type_value.lower()
            if mime_type == "text/plain":
                text_bodies.append(decoded)
            elif mime_type == "text/html":
                html_bodies.append(decoded)
        parts = part.get("parts") or []
        if not isinstance(parts, list):
            raise ValueError("Gmail message parts are invalid")
        for child in parts:
            if not isinstance(child, dict):
                raise ValueError("Gmail message part is invalid")
            visit(child)

    visit(payload)
    to = _decode_address_header(header_values.get("to", ""))
    try:
        subject = str(make_header(decode_header(header_values.get("subject", ""))))
    except (UnicodeError, LookupError):
        subject = header_values.get("subject", "")
    if not isinstance(resource.get("snippet", ""), str):
        raise ValueError("Gmail snippet is invalid")
    label_ids = resource.get("labelIds")
    if label_ids is None:
        label_ids = []
    if not isinstance(label_ids, list) or any(not isinstance(label, str) for label in label_ids):
        raise ValueError("Gmail labels are invalid")
    return Message(
        message_id=resource["id"],
        thread_id=resource["threadId"],
        rfc_message_id=header_values.get("message-id", ""),
        in_reply_to=header_values.get("in-reply-to", ""),
        references=header_values.get("references", ""),
        from_address=header_values.get("from", ""),
        reply_to=header_values.get("reply-to", ""),
        to=to,
        subject=subject,
        text_body="\n".join(text_bodies),
        html_body="\n".join(html_bodies),
        snippet=resource.get("snippet", ""),
        received_at=received_at,
        label_ids=label_ids,
    )


def parse_mailbox(value: str) -> str:
    """Validate one RFC mailbox while refusing header injection and empty addresses."""
    return str(_parse_mailbox_address(value))


def _parse_mailbox_address(value: str) -> Address:
    if "\r" in value or "\n" in value:
        raise ValueError("mailbox contains a newline")
    if _CONTROL_CHARACTER.search(value) is not None:
        raise ValueError("mailbox contains a control character")
    parsed = Parser(policy=policy.default).parsestr(f"To: {value}\n\n")
    addresses: list[Address] = list(parsed["To"].addresses) if parsed["To"] is not None else []
    if len(addresses) != 1 or not addresses[0].addr_spec or "@" not in addresses[0].addr_spec:
        raise ValueError("invalid mailbox")
    return addresses[0]


def _format_go_mailbox(value: str) -> str:
    """Validate one mailbox and format it like Go's mail.Address.String, as the Go connector sends it."""
    parsed = _parse_mailbox_address(value)
    plain_mailbox = _parse_plain_mailbox(value)
    # The standard parser keeps the space between adjacent encoded words, which Go and RFC 2047 drop.
    display_name = plain_mailbox[0] if plain_mailbox is not None else parsed.display_name
    return _format_go_display_name_and_address(display_name, parseaddr(str(parsed))[1])


def _decode_address_header(value: str) -> list[str]:
    """Format each mailbox of a To header like Go, or none when any of them is invalid."""
    if not value or "\r" in value or "\n" in value:
        return []
    mailboxes: list[str] = []
    for entry in _split_address_list(value):
        plain_mailbox = _parse_plain_mailbox(entry)
        if plain_mailbox is None:
            return []
        mailboxes.append(_format_go_display_name_and_address(*plain_mailbox))
    return mailboxes


def _split_address_list(value: str) -> list[str]:
    # A comma outside a quoted string ends a mailbox; inside one, a backslash escapes the next character.
    addresses: list[str] = []
    is_quoted = False
    start = 0
    index = 0
    while index < len(value):
        character = value[index]
        if is_quoted and character == "\\":
            index += 1
        elif character == '"':
            is_quoted = not is_quoted
        elif character == "," and not is_quoted:
            addresses.append(value[start:index].strip())
            start = index + 1
        index += 1
    addresses.append(value[start:].strip())
    return addresses


def _parse_plain_mailbox(value: str) -> tuple[str, str] | None:
    """Return the name and address of a mailbox the TypeScript port accepts, decoding the name like Go."""
    if _CONTROL_CHARACTER.search(value) is not None:
        return None
    trimmed = value.strip()
    angle = _MAILBOX_WITH_ANGLE_ADDRESS.fullmatch(trimmed)
    if angle is None and ("<" in trimmed or ">" in trimmed):
        return None
    address = angle.group(2).strip() if angle is not None else trimmed
    if _PLAIN_MAILBOX_ADDRESS.fullmatch(address) is None:
        return None
    display = angle.group(1).strip() if angle is not None else ""
    if display.startswith('"'):
        if _QUOTED_DISPLAY_NAME.fullmatch(display) is None:
            return None
        return re.sub(r"\\(.)", r"\1", display[1:-1], flags=re.DOTALL), address
    if _UNQUOTED_DISPLAY_NAME_SPECIAL.search(display) is not None:
        return None
    decoded_display = _decode_display_name_phrase(display) if display else ""
    return None if decoded_display is None else (decoded_display, address)


def _decode_display_name_phrase(phrase: str) -> str | None:
    # Go's net/mail joins words by one space, adjacent encoded words by none, and rejects an empty result.
    words: list[str] = []
    encoded_run = ""
    for word in re.split(r"[ \t]+", phrase):
        if not word:
            continue
        decoded = _decode_encoded_word(word)
        if decoded is not None:
            encoded_run += decoded
            continue
        if encoded_run:
            words.append(encoded_run)
        encoded_run = ""
        words.append(word)
    if encoded_run:
        words.append(encoded_run)
    return " ".join(words) if words else None


def _decode_encoded_word(word: str) -> str | None:
    # Go's mime.WordDecoder rules; None leaves the word literal.
    match = _ENCODED_WORD.fullmatch(word)
    if match is None:
        return None
    charset, encoding, text = match.groups()
    content = _decode_b_encoded_text(text) if encoding in "Bb" else _decode_q_encoded_text(text)
    return None if content is None else _decode_charset_bytes(charset, content)


def _decode_b_encoded_text(text: str) -> bytes | None:
    return base64.b64decode(text) if _PADDED_BASE64.fullmatch(text) is not None else None


def _decode_q_encoded_text(text: str) -> bytes | None:
    content = bytearray()
    index = 0
    while index < len(text):
        character = text[index]
        if character == "_":
            content.append(0x20)
        elif character == "=":
            hex_digits = text[index + 1:index + 3]
            if _Q_ENCODED_BYTE.fullmatch(hex_digits) is None:
                return None
            content.append(int(hex_digits, 16))
            index += 2
        elif " " <= character <= "~":
            content.append(ord(character))
        else:
            return None
        index += 1
    return bytes(content)


def _decode_charset_bytes(charset: str, content: bytes) -> str | None:
    match charset.lower():
        case "utf-8":
            return content.decode("utf-8", errors="replace")
        case "iso-8859-1":
            return content.decode("latin-1")
        case "us-ascii":
            return "".join(chr(byte) if byte < 0x80 else "\ufffd" for byte in content)
    # Go rejects other charsets; a known codec decodes them, as TypeScript's TextDecoder does.
    try:
        return content.decode(charset)
    except (LookupError, ValueError):
        # An unknown charset, invalid bytes, or a codec such as idna that raises UnicodeError.
        return None


def _format_go_display_name_and_address(display_name: str, address: str) -> str:
    if not display_name:
        return f"<{address}>"
    return f"{_format_go_display_name(display_name)} <{address}>"


def _format_go_display_name(display_name: str) -> str:
    # A lone surrogate becomes U+FFFD, as Go's JSON decoder and the TypeScript UTF-8 encoder make it.
    display_name = display_name.encode("utf-16", "surrogatepass").decode("utf-16", "replace")
    if _QUOTABLE_DISPLAY_NAME.fullmatch(display_name) is not None:
        return '"' + display_name.replace("\\", "\\\\").replace('"', '\\"') + '"'
    if _B_ENCODED_DISPLAY_NAME_CHARACTER.search(display_name) is not None:
        return _encode_b_words(display_name)
    return _encode_q_words(display_name)


def _encode_q_words(display_name: str) -> str:
    # Go's mime.QEncoding splitting: never inside a character, and only where the next one would pass 63.
    words: list[str] = []
    word = ""
    for character in display_name:
        if " " <= character <= "~" and character not in "=?_":
            encoded = "_" if character == " " else character
        else:
            encoded = "".join(f"={byte:02X}" for byte in character.encode("utf-8"))
        if len(word) + len(encoded) > _MAX_ENCODED_WORD_TEXT_LENGTH:
            words.append(word)
            word = ""
        word += encoded
    words.append(word)
    return " ".join(f"=?utf-8?q?{text}?=" for text in words)


def _encode_b_words(display_name: str) -> str:
    # Go's mime.BEncoding splitting: whole characters, at most 45 bytes per word.
    words: list[bytes] = [b""]
    for character in display_name:
        encoded = character.encode("utf-8")
        if len(words[-1]) + len(encoded) > _MAX_B_ENCODED_WORD_BYTES:
            words.append(b"")
        words[-1] += encoded
    return " ".join(f"=?utf-8?b?{base64.b64encode(word).decode()}?=" for word in words)


def validate_send_input(input_value: SendMessageInput, sender: str) -> list[str]:
    """Validate new-mail fields and return distinct parsed recipient mailboxes."""
    if (
        not sender.strip()
        or not input_value.subject.strip()
        or not input_value.text_body.strip()
        or not input_value.to
    ):
        raise ValueError("sender, recipients, subject, and text body are required")
    if "\r" in input_value.subject or "\n" in input_value.subject:
        raise ValueError("sender, recipients, subject, and text body are required")
    try:
        sender_address = parse_mailbox(sender)
    except ValueError as error:
        raise ValueError("authorized primary email is invalid") from error
    if sender_address.lower() != sender.strip().lower():
        raise ValueError("authorized primary email is invalid")
    try:
        recipients = [parse_mailbox(value) for value in input_value.to]
    except ValueError as error:
        raise ValueError("recipient email is invalid") from error
    canonical = [parseaddr(value)[1].lower() for value in recipients]
    if len(set(canonical)) != len(canonical):
        raise ValueError("recipient email is duplicated")
    return [_format_go_mailbox(value) for value in input_value.to]


def rfc_message_id_address(call: Call, run_id: str | None, flow_started_at: datetime | None = None) -> str:
    """Derive a stable run-scoped Message-ID, retaining Go parity for valid Flow start times."""
    if not isinstance(run_id, str) or not run_id:
        raise TypeError("Dex run ID is required")
    try:
        seconds = flow_started_at.timestamp() if isinstance(flow_started_at, datetime) else math.nan
    except (OverflowError, OSError, ValueError):
        seconds = math.nan
    run_identity = f"{run_id}\0{math.floor(seconds) if math.isfinite(seconds) else 'no-flow-start'}"
    digest = hashlib.sha256(f"{call.idempotency_key}\0{run_identity}".encode()).hexdigest()
    return f"{digest}@dex.superdurable.dev"


def sent_message_search_query(
    call: Call, run_id: str | None, flow_started_at: datetime | None,
    *, message_id_address: str | None = None,
) -> str:
    """Search Sent Mail by deterministic RFC Message-ID without angle brackets.

    Gmail replaces Message-ID on send, so this query finds no message the connector sent; read-back no
    longer uses it. It is kept so code that imported it keeps working.
    """
    return "in:sent rfc822msgid:" + (
        message_id_address if message_id_address is not None
        else rfc_message_id_address(call, run_id, flow_started_at)
    )


def _sent_mail_candidate_query(recipients: list[str]) -> str:
    query = "in:sent " + _SENT_MAIL_CANDIDATE_WINDOW
    if not recipients:
        return query
    address = parseaddr(recipients[0])[1]
    if not _SEARCHABLE_RECIPIENT_ADDRESS.fullmatch(address):
        return query
    return query + " to:" + address


def _crlf(value: str) -> str:
    return value.replace("\r\n", "\n").replace("\r", "\n").replace("\n", "\r\n")


def _q_encode_subject(subject: str) -> str:
    if all(32 <= ord(character) <= 126 for character in subject):
        return subject
    return header_encode(subject.encode("utf-8"), charset="UTF-8")


def _mime_message(
    call: Call,
    sender: str,
    recipient: str,
    subject: str,
    text_body: str,
    html_body: str,
    run_id: str,
    flow_started_at: datetime | None,
    reply_headers: tuple[str, str] = ("", ""),
    *, message_id_address: str | None = None,
) -> bytes:
    address = message_id_address if message_id_address is not None else rfc_message_id_address(
        call, run_id, flow_started_at
    )
    lines = [
        f"From: {sender}",
        f"To: {recipient}",
        f"Subject: {_q_encode_subject(subject)}",
        f"Message-ID: <{address}>",
        f"{_DEX_MESSAGE_ID_HEADER}: <{address}>",
    ]
    if reply_headers[0]:
        lines.append(f"In-Reply-To: {reply_headers[0]}")
        lines.append(f"References: {reply_headers[1]}")
    lines.extend((f"X-Dex-Call-ID: {call.id}", "MIME-Version: 1.0"))
    if not html_body:
        lines.extend(('Content-Type: text/plain; charset="UTF-8"', "Content-Transfer-Encoding: 8bit"))
        return ("\r\n".join(lines) + "\r\n\r\n" + _crlf(text_body)).encode()
    boundary = "dex-" + hashlib.sha256(str(call.idempotency_key).encode()).hexdigest()[:24]
    lines.append(f'Content-Type: multipart/alternative; boundary="{boundary}"')
    lines.append("")
    for media_type, body in (("text/plain", text_body), ("text/html", html_body)):
        lines.extend((f"--{boundary}", f'Content-Type: {media_type}; charset="UTF-8"', "", _crlf(body)))
    lines.extend((f"--{boundary}--", ""))
    return "\r\n".join(lines).encode()


def build_send_message(
    call: Call,
    sender: str,
    recipients: list[str],
    input_value: SendMessageInput,
    run_id: str,
    flow_started_at: datetime | None,
    *, message_id_address: str | None = None,
) -> bytes:
    """Encode Go-compatible messages.send JSON with a deterministic MIME body."""
    raw = _mime_message(
        call,
        sender,
        ", ".join(recipients),
        input_value.subject,
        input_value.text_body,
        input_value.html_body,
        run_id,
        flow_started_at,
        message_id_address=message_id_address,
    )
    return json.dumps(
        {"raw": base64.urlsafe_b64encode(raw).rstrip(b"=").decode()}, separators=(",", ":")
    ).encode()


def build_reply_message(
    call: Call,
    sender: str,
    recipient: str,
    source: Message,
    input_value: ReplyToMessageInput,
    run_id: str,
    flow_started_at: datetime | None,
    *, message_id_address: str | None = None,
) -> bytes:
    """Encode a threaded messages.send body with source reply headers."""
    subject = source.subject if source.subject.strip().lower().startswith("re:") else "Re: " + source.subject
    references = (source.references + " " + source.rfc_message_id).strip()
    raw = _mime_message(
        call,
        sender,
        recipient,
        subject,
        input_value.text_body,
        input_value.html_body,
        run_id,
        flow_started_at,
        (source.rfc_message_id, references),
        message_id_address=message_id_address,
    )
    encoded = base64.urlsafe_b64encode(raw).rstrip(b"=").decode()
    return json.dumps({"raw": encoded, "threadId": source.thread_id}, separators=(",", ":")).encode()


class GmailProvider:
    """Call Gmail with bounded bodies and at most one credential refresh after 401."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        refresh_driver: CredentialRefreshDriver[Credentials] | None = None,
        http_client: httpx.AsyncClient | None = None,
        *,
        logger: TriggerLogger | None = None,
    ) -> None:
        """Validate settings and keep the credential source, transport, and Trigger logger.

        logger receives the Gmail Trigger pollers' records: failed polls, ignored and skipped messages,
        delivery retries, and the durable inboxes `new_project_message_trigger_runner` creates. None
        uses the standard logging adapter. Records carry message and thread IDs, never senders,
        subjects, snippets, bodies, or tokens.
        """
        if credentials is None:
            raise ValueError("credential provider is required")
        self.endpoint = validate_base_url(config.endpoint or Config().endpoint)
        self.max_response_bytes = config.max_response_bytes or Config().max_response_bytes
        self.max_message_bytes = config.max_message_bytes or Config().max_message_bytes
        if (
            self.max_response_bytes < 1
            or self.max_message_bytes < 1
            or config.poll_interval_seconds < 1
            or config.poll_page_size < 1
            or config.poll_page_size > 100
        ):
            raise ValueError("Gmail response limits and polling configuration are invalid")
        # Delay between Trigger polls and the recent inbox messages each poll inspects.
        self.poll_interval = timedelta(seconds=config.poll_interval_seconds)
        self.poll_page_size = config.poll_page_size
        self.trigger_logger = logger
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        self._owns_http_client = http_client is None
        self.refresh_driver = refresh_driver or GmailCredentialRefreshDriver(self.http_client)

    async def close(self) -> None:
        """Close the internally constructed HTTP client; a caller-owned client stays open."""
        if self._owns_http_client:
            await self.http_client.aclose()

    async def resolve_credentials(self, call: Call) -> Credentials:
        """Resolve current secret material and verify the authorized mailbox."""
        try:
            credential = await resolve_credential(self.credentials, call, self.refresh_driver)
        except ValueError as error:
            # The SDK project provider replaces a decoder error but keeps it as the context.
            if isinstance(error, StoredCredentialMaterialError) or isinstance(
                error.__context__, StoredCredentialMaterialError
            ):
                raise GmailCredentialMaterialError("Gmail stored credential material is invalid") from None
            raise
        try:
            self._validate_credentials(credential)
        except ValueError as error:
            raise GmailCredentialMaterialError(str(error)) from None
        except (TypeError, AttributeError):
            # Malformed fields from a credential source are invalid material, not a transient failure.
            raise GmailCredentialMaterialError("Gmail credential fields are invalid") from None
        return credential

    def _validate_credentials(self, credential: Credentials) -> None:
        if not is_header_safe_credential(credential.access_token.reveal()):
            raise ValueError("Gmail access token is invalid")
        if parse_mailbox(credential.primary_email).lower() != credential.primary_email.strip().lower():
            raise ValueError("Gmail primary email is invalid")
        if credential.auth_method_id not in ("", "google-oauth", "workspace-domain-delegation"):
            raise ValueError("Gmail authorization method is invalid")

    async def request(
        self,
        call: Call,
        credential: Credentials,
        method: str,
        path: str,
        *,
        query: dict[str, str | list[str]] | None = None,
        payload: bytes | None = None,
        observed_statuses: list[int] | None = None,
    ) -> tuple[GmailHTTPResult, Credentials]:
        """Send once, bounded by providerhttp, then retry one rejected token if supported.

        ``observed_statuses`` receives each response status before its body is read, so a caller
        whose deadline interrupts the body still knows Gmail answered.
        """
        target = self.endpoint + path
        if query:
            target += "?" + urlencode(query, doseq=True)
        for attempt in range(2):
            headers = {"Authorization": "Bearer " + credential.access_token.reveal()}
            if payload is not None:
                headers["Content-Type"] = "application/json"
            request = self.http_client.build_request(method, target, headers=headers, content=payload)
            response, _ = await send_traced(self.http_client, request)
            if (
                response.status_code == 401
                and attempt == 0
                and isinstance(self.credentials, RejectedCredentialRefreshingProvider)
            ):
                try:
                    next_credential = await resolve_credential_after_rejection(
                        self.credentials, call, self.refresh_driver
                    )
                    self._validate_credentials(next_credential)
                except Exception:
                    pass
                else:
                    await response.aclose()
                    credential = next_credential
                    continue
            if observed_statuses is not None:
                observed_statuses.append(response.status_code)
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError as error:
                raise GmailResponseTooLarge(response.status_code, response.headers) from error
            except ProviderTransportError as error:
                raise GmailResponseReadFailure(response.status_code, response.headers) from error
            result = GmailHTTPResult(response.status_code, response.headers, body)
            return result, credential
        raise RuntimeError("Gmail authenticated request retry was exhausted")

    async def read_message(
        self,
        call: Call,
        credential: Credentials,
        message_id: str,
        format_name: str,
    ) -> tuple[GmailHTTPResult, Credentials]:
        """Read full, metadata, or minimal Gmail message format with safe URL encoding."""
        query: dict[str, str | list[str]] = {"format": format_name}
        if format_name == "metadata":
            query["metadataHeaders"] = [
                "Message-ID",
                "In-Reply-To",
                "References",
                "From",
                "Reply-To",
                "To",
                "Subject",
            ]
        return await self.request(
            call, credential, "GET", "/users/me/messages/" + quote(message_id, safe=""), query=query
        )

    async def list_inbox_messages(
        self, call: Call, credential: Credentials, search_query: str
    ) -> tuple[list[tuple[str, str]], Credentials]:
        """List the newest inbox page matching search_query as (message ID, thread ID) pairs, newest first.

        Raises ValueError for a non-2xx or unusable response, and the transport errors `request` raises.
        """
        # Go's url.Values.Encode sorts the keys: labelIds, maxResults, q.
        query: dict[str, str | list[str]] = {"labelIds": "INBOX", "maxResults": str(self.poll_page_size)}
        if search_query.strip():
            query["q"] = search_query
        result, credential = await self.request(call, credential, "GET", "/users/me/messages", query=query)
        if result.status_code < 200 or result.status_code >= 300:
            raise ValueError("Gmail message list is unavailable")
        try:
            document = json.loads(result.body)
        except (ValueError, UnicodeDecodeError) as error:
            raise ValueError("Gmail message list is invalid") from error
        listed = document.get("messages") if isinstance(document, dict) else None
        if not isinstance(document, dict) or not isinstance(listed, list | None):
            raise ValueError("Gmail message list is invalid")
        references: list[tuple[str, str]] = []
        for item in listed or []:
            # Go decodes a null entry as an empty reference, which the poller skips.
            if item is None:
                references.append(("", ""))
                continue
            message_id = item.get("id") if isinstance(item, dict) else None
            thread_id = item.get("threadId") if isinstance(item, dict) else None
            if not isinstance(item, dict) or not isinstance(message_id, str | None):
                raise ValueError("Gmail message list is invalid")
            if not isinstance(thread_id, str | None):
                raise ValueError("Gmail message list is invalid")
            references.append((message_id or "", thread_id or ""))
        return references, credential

    async def find_sent_message(
        self,
        call: Call,
        credential: Credentials,
        run_id: str | None,
        flow_started_at: datetime | None,
        *, message_id_address: str | None = None,
    ) -> tuple[dict[str, str] | None, Credentials]:
        """Return the Sent Mail message carrying the run-specific Message-ID, or None.

        Gmail replaces Message-ID on send and cannot search custom headers, so this lists up to 25
        Sent Mail messages from the last two days and reads each one's X-Dex-Message-ID and
        Message-ID headers; either header equal to the bracketed address matches. The returned
        credential is the latest one, which a refresh after a 401 may replace. Raises ValueError, or
        the transport and body errors `request` raises, when the search fails or a candidate cannot
        be read and no other candidate matches.
        """
        read_back = _SentMailReadBack(
            self, call, credential,
            message_id_address if message_id_address is not None
            else rfc_message_id_address(call, run_id, flow_started_at),
            [],
        )
        return await read_back.find(), read_back.credential


class _SentMailReadBack:
    # Gmail replaces Message-ID on send and cannot search custom headers, so candidates are read.

    def __init__(
        self, provider: GmailProvider, call: Call, credential: Credentials, message_id_address: str,
        recipients: list[str],
    ) -> None:
        self._provider = provider
        self._call = call
        # The latest credential, which a refresh after a 401 replaces.
        self.credential = credential
        self._search_query = _sent_mail_candidate_query(recipients)
        self._dex_message_id = f"<{message_id_address}>"
        # Candidates already read without a match; a message's headers never change.
        self._mismatched_candidate_ids: set[str] = set()

    async def find(self) -> dict[str, str] | None:
        candidate_ids = await self._list_candidates()
        candidate_error: ValueError | OSError | None = None
        for candidate_id in candidate_ids:
            if not candidate_id or candidate_id in self._mismatched_candidate_ids:
                continue
            try:
                sent = await self._read_candidate(candidate_id)
            except (ValueError, OSError) as error:
                candidate_error = error
                continue
            if sent is not None:
                return sent
            self._mismatched_candidate_ids.add(candidate_id)
        if candidate_error is not None:
            raise candidate_error
        return None

    async def _list_candidates(self) -> list[str]:
        result, self.credential = await self._provider.request(
            self._call,
            self.credential,
            "GET",
            "/users/me/messages",
            query={
                "q": self._search_query,
                "maxResults": str(_SENT_MAIL_CANDIDATE_LIMIT),
                "includeSpamTrash": "true",
            },
        )
        if result.status_code != 200:
            raise ValueError("Gmail sent-message lookup did not return a usable response")
        try:
            document = json.loads(result.body)
        except (ValueError, UnicodeDecodeError) as error:
            raise ValueError("Gmail sent-message lookup returned an invalid response") from error
        # Go decodes a null document, list, ID, or thread ID as empty; another type is an invalid response.
        listed = document.get("messages") if isinstance(document, dict) else None
        if not isinstance(document, dict | None) or not isinstance(listed, list | None):
            raise ValueError("Gmail sent-message lookup returned an invalid response")
        candidate_ids: list[str] = []
        for candidate in listed or []:
            candidate_id = candidate.get("id") if isinstance(candidate, dict) else None
            thread_id = candidate.get("threadId") if isinstance(candidate, dict) else None
            if not isinstance(candidate, dict | None) or not isinstance(candidate_id, str | None):
                raise ValueError("Gmail sent-message lookup returned an invalid response")
            if not isinstance(thread_id, str | None):
                raise ValueError("Gmail sent-message lookup returned an invalid response")
            candidate_ids.append(candidate_id or "")
        return candidate_ids

    async def _read_candidate(self, message_id: str) -> dict[str, str] | None:
        result, self.credential = await self._provider.request(
            self._call,
            self.credential,
            "GET",
            "/users/me/messages/" + quote(message_id, safe=""),
            query={"format": "metadata", "metadataHeaders": ["Message-ID", _DEX_MESSAGE_ID_HEADER]},
        )
        if result.status_code == 404:
            # The candidate was deleted after the search; it cannot be this call's copy any more.
            return None
        if result.status_code != 200:
            raise ValueError("Gmail sent-message header read did not return a usable response")
        try:
            resource = json.loads(result.body)
        except (ValueError, UnicodeDecodeError) as error:
            raise ValueError("Gmail sent-message header read returned an invalid response") from error
        for name, value in _sent_mail_candidate_headers(resource, message_id):
            is_message_id_header = name.lower() in ("message-id", _DEX_MESSAGE_ID_HEADER.lower())
            if is_message_id_header and value.strip() == self._dex_message_id:
                return {"id": message_id, "threadId": resource.get("threadId") or ""}
        return None


def _sent_mail_candidate_headers(resource: object, message_id: str) -> list[tuple[str, str]]:
    """Return a metadata read's headers, rejecting another message or a resource Go cannot decode."""
    invalid = ValueError("Gmail sent-message header read returned an invalid response")
    if not isinstance(resource, dict) or resource.get("id") != message_id:
        raise invalid
    string_fields = ("threadId", "snippet", "internalDate")
    if not all(isinstance(resource.get(name), str | None) for name in string_fields):
        raise invalid
    label_ids = resource.get("labelIds")
    if not isinstance(label_ids, list | None):
        raise invalid
    if not all(isinstance(label, str | None) for label in label_ids or []):
        raise invalid
    payload = resource.get("payload")
    # Go's gmailMessageResource at every depth: null decodes as empty, any other mistyped field fails.
    pending_parts: list[object] = [payload]
    while pending_parts:
        part = pending_parts.pop()
        if part is None:
            continue
        if not isinstance(part, dict) or not isinstance(part.get("mimeType"), str | None):
            raise invalid
        body = part.get("body")
        if not isinstance(body, dict | None) or not isinstance((body or {}).get("data"), str | None):
            raise invalid
        if not _is_go_header_list(part.get("headers")):
            raise invalid
        children = part.get("parts")
        if not isinstance(children, list | None):
            raise invalid
        pending_parts.extend(children or [])
    listed = payload.get("headers") if isinstance(payload, dict) else None
    return [
        (header.get("name") or "", header.get("value") or "")
        for header in listed or []
        if isinstance(header, dict)
    ]


def _is_go_header_list(headers: object) -> bool:
    if not isinstance(headers, list | None):
        return False
    return all(
        header is None
        or (
            isinstance(header, dict)
            and isinstance(header.get("name"), str | None)
            and isinstance(header.get("value"), str | None)
        )
        for header in headers or []
    )


__all__ = [
    "BodyTooLargeError",
    "GmailHTTPResult",
    "GmailProvider",
    "GmailResponseTooLarge",
    "GmailResponseReadFailure",
    "ProviderTransportError",
    "build_reply_message",
    "build_send_message",
    "decode_message",
    "parse_mailbox",
    "rfc_message_id_address",
    "sent_message_search_query",
    "validate_send_input",
]
