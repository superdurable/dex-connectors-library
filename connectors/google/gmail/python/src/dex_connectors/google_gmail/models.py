# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Gmail configuration, credentials, request values, and response values."""

from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import SecretString
from dex_connectors.sdk.providerhttp import validate_base_url

from ._generated.config import Config as GeneratedConfig


class Config(GeneratedConfig):
    """Gmail API settings; zero-valued limits select the manifest defaults.

    The endpoint, response limit, message limit, poll interval, and page size
    inherit their defaults from the generated connection contract.
    """

    __slots__ = ()

    @classmethod
    def from_project_settings(cls, settings: dict[str, Any]) -> "Config":
        """Decode manifest field names from one Dex Web connection configuration."""
        if not isinstance(settings, dict) or set(settings) - {
            "endpoint",
            "maxResponseBytes",
            "maxMessageBytes",
            "pollInterval",
            "pollPageSize",
        }:
            raise ValueError("Gmail connection settings are invalid")
        defaults = cls()
        endpoint = settings.get("endpoint") or defaults.endpoint
        response_bytes = settings.get("maxResponseBytes") or defaults.max_response_bytes
        message_bytes = settings.get("maxMessageBytes") or defaults.max_message_bytes
        poll_interval_ns = settings.get("pollInterval") or int(defaults.poll_interval_seconds * 1_000_000_000)
        poll_page_size = settings.get("pollPageSize") or defaults.poll_page_size
        if (
            not isinstance(endpoint, str)
            or not isinstance(response_bytes, int)
            or isinstance(response_bytes, bool)
            or not isinstance(message_bytes, int)
            or isinstance(message_bytes, bool)
            or not isinstance(poll_interval_ns, int)
            or isinstance(poll_interval_ns, bool)
            or not isinstance(poll_page_size, int)
            or isinstance(poll_page_size, bool)
        ):
            raise ValueError("Gmail connection settings have invalid types")
        result = cls(
            validate_base_url(endpoint),
            response_bytes,
            message_bytes,
            poll_interval_ns / 1_000_000_000,
            poll_page_size,
        )
        if result.max_response_bytes < 1 or result.max_message_bytes < 1 or result.poll_interval_seconds < 1:
            raise ValueError("Gmail connection limits and poll interval are invalid")
        if result.poll_page_size < 1 or result.poll_page_size > 100:
            raise ValueError("Gmail poll page size is invalid")
        return result


@dataclass(frozen=True, slots=True, repr=False)
class Credentials:
    """Secret-bearing Google authorization material resolved at provider call time."""

    # Short-lived bearer token; never persist it in Flow state.
    access_token: SecretString
    # Verified mailbox identity from OAuth or Workspace delegation.
    primary_email: str
    # Manifest authorization method identity.
    auth_method_id: str = "google-oauth"
    # OAuth application client ID.
    oauth_client_id: str = ""
    # OAuth application client secret.
    oauth_client_secret: SecretString = field(default_factory=lambda: SecretString(""))
    # Renewable OAuth grant token.
    refresh_token: SecretString = field(default_factory=lambda: SecretString(""))
    # Workspace service-account JSON key.
    service_account_key: SecretString = field(default_factory=lambda: SecretString(""))
    # Administrator-approved Workspace mailbox.
    delegated_user: str = ""


@dataclass(frozen=True, slots=True)
class GetMessageInput:
    """Gmail message ID to fetch in full format."""

    # Provider message identifier.
    message_id: str = field(metadata={"dex_json_name": "messageId"})


@dataclass(frozen=True, slots=True)
class SendMessageInput:
    """Recipients and body for a new message from the authorized mailbox."""

    # Complete recipient mailboxes.
    to: list[str]
    # RFC 5322 subject without line breaks.
    subject: str
    # Required plain-text body.
    text_body: str = field(metadata={"dex_json_name": "textBody"})
    # Optional HTML alternative body.
    html_body: str = field(
        default="", metadata={"dex_json_name": "htmlBody", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class ReplyToMessageInput:
    """Source message ID and body for a reply in its Gmail thread."""

    # Provider source message identifier.
    message_id: str = field(metadata={"dex_json_name": "messageId"})
    # Required plain-text reply body.
    text_body: str = field(metadata={"dex_json_name": "textBody"})
    # Optional HTML alternative reply body.
    html_body: str = field(
        default="", metadata={"dex_json_name": "htmlBody", "dex_json_omit_default": True}
    )


@dataclass(frozen=True, slots=True)
class SendMessageOutput:
    """Accepted send identity; IDs may be empty after an unusable 2xx response."""

    # Authorized primary sender mailbox.
    sender: str = ""
    # Resolved recipient mailboxes; empty after early retry read-back.
    recipients: list[str] = field(default_factory=list)
    # Gmail message ID, possibly empty after an unusable accepted response.
    message_id: str = field(default="", metadata={"dex_json_name": "messageId"})
    # Gmail thread ID, possibly empty for a new accepted message.
    thread_id: str = field(default="", metadata={"dex_json_name": "threadId"})


@dataclass(frozen=True, slots=True)
class Message:
    """Decoded Gmail message headers, bodies, labels, and receipt time."""

    # Gmail message identifier.
    message_id: str = field(default="", metadata={"dex_json_name": "messageId"})
    # Gmail conversation thread identifier.
    thread_id: str = field(default="", metadata={"dex_json_name": "threadId"})
    # RFC Message-ID header, if supplied.
    rfc_message_id: str = field(
        default="", metadata={"dex_json_name": "rfcMessageId", "dex_json_omit_default": True}
    )
    # In-Reply-To header, if supplied.
    in_reply_to: str = field(
        default="", metadata={"dex_json_name": "inReplyTo", "dex_json_omit_default": True}
    )
    # References header, if supplied.
    references: str = field(default="", metadata={"dex_json_omit_default": True})
    # Decoded sender mailbox header.
    from_address: str = field(default="", metadata={"dex_json_name": "from"})
    # Reply-To header, if supplied.
    reply_to: str = field(
        default="", metadata={"dex_json_name": "replyTo", "dex_json_omit_default": True}
    )
    # Decoded recipient mailbox headers.
    to: list[str] = field(default_factory=list, metadata={"dex_json_omit_default": True})
    # Decoded message subject.
    subject: str = ""
    # Concatenated plain-text MIME parts.
    text_body: str = field(
        default="", metadata={"dex_json_name": "textBody", "dex_json_omit_default": True}
    )
    # Concatenated HTML MIME parts.
    html_body: str = field(
        default="", metadata={"dex_json_name": "htmlBody", "dex_json_omit_default": True}
    )
    # Gmail preview snippet.
    snippet: str = field(default="", metadata={"dex_json_omit_default": True})
    # Gmail internal timestamp in UTC.
    received_at: datetime = field(
        default=datetime.min.replace(tzinfo=UTC),
        metadata={"dex_json_name": "receivedAt", "dex_json_format": "rfc3339"},
    )
    # Gmail label identifiers.
    label_ids: list[str] = field(
        default_factory=list,
        metadata={"dex_json_name": "labelIds", "dex_json_omit_default": True},
    )
