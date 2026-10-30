# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Gmail HTTP calls, MIME construction, and Sent Mail read-back helpers."""

import base64
import binascii
import hashlib
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from email import policy
from email.header import decode_header, make_header
from email.parser import Parser
from email.quoprimime import header_encode
from email.utils import getaddresses, parseaddr
from urllib.parse import quote, urlencode

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    CredentialRefreshDriver,
    RejectedCredentialRefreshingProvider,
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


def _base64url_decode(value: str) -> str:
    return base64.b64decode(value + "=" * (-len(value) % 4), altchars=b"-_", validate=True).decode(
        "utf-8", errors="replace"
    )


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
    address_list = getaddresses([header_values.get("to", "")])
    to = [f"{name} <{address}>" if name else address for name, address in address_list if address]
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
    if "\r" in value or "\n" in value:
        raise ValueError("mailbox contains a newline")
    parsed = Parser(policy=policy.default).parsestr(f"To: {value}\n\n")
    addresses = list(parsed["To"].addresses) if parsed["To"] is not None else []
    if len(addresses) != 1 or not addresses[0].addr_spec or "@" not in addresses[0].addr_spec:
        raise ValueError("invalid mailbox")
    address = addresses[0]
    return str(address)


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
    return recipients


def rfc_message_id_address(call: Call, run_id: str, flow_started_at: datetime) -> str:
    """Derive Go's run-scoped Message-ID address from the stable call key."""
    run_identity = f"{run_id}\0{int(flow_started_at.timestamp())}"
    digest = hashlib.sha256(f"{call.idempotency_key}\0{run_identity}".encode()).hexdigest()
    return f"{digest}@dex.superdurable.dev"


def sent_message_search_query(call: Call, run_id: str, flow_started_at: datetime) -> str:
    """Search Sent Mail by deterministic RFC Message-ID without angle brackets."""
    return "in:sent rfc822msgid:" + rfc_message_id_address(call, run_id, flow_started_at)


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
    flow_started_at: datetime,
    reply_headers: tuple[str, str] = ("", ""),
) -> bytes:
    lines = [
        f"From: {sender}",
        f"To: {recipient}",
        f"Subject: {_q_encode_subject(subject)}",
        f"Message-ID: <{rfc_message_id_address(call, run_id, flow_started_at)}>",
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
    lines.extend(("", ""))
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
    flow_started_at: datetime,
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
    flow_started_at: datetime,
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
    ) -> None:
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
        credential = await resolve_credential(self.credentials, call, self.refresh_driver)
        self._validate_credentials(credential)
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
    ) -> tuple[GmailHTTPResult, Credentials]:
        """Send once, bounded by providerhttp, then retry one rejected token if supported."""
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

    async def find_sent_message(
        self,
        call: Call,
        credential: Credentials,
        run_id: str,
        flow_started_at: datetime,
    ) -> tuple[dict[str, str] | None, Credentials]:
        """Return first Sent Mail match for the run-specific RFC Message-ID."""
        result, credential = await self.request(
            call,
            credential,
            "GET",
            "/users/me/messages",
            query={
                "q": sent_message_search_query(call, run_id, flow_started_at),
                "maxResults": "1",
                "includeSpamTrash": "true",
            },
        )
        if result.status_code != 200:
            raise ValueError("Gmail sent-message lookup did not return a usable response")
        try:
            document = json.loads(result.body)
        except (ValueError, UnicodeDecodeError) as error:
            raise ValueError("Gmail sent-message lookup returned an invalid response") from error
        if not isinstance(document, dict) or not isinstance(document.get("messages", []), list):
            raise ValueError("Gmail sent-message lookup returned an invalid response")
        for match in document.get("messages", []):
            if (
                not isinstance(match, dict)
                or not isinstance(match.get("id", ""), str)
                or not isinstance(match.get("threadId", ""), str)
            ):
                raise ValueError("Gmail sent-message lookup returned an invalid response")
            if match.get("id"):
                return {"id": match["id"], "threadId": match.get("threadId", "")}, credential
        return None, credential


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
