# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verified TLS transport and credential handling for IMAP and SMTP."""

import imaplib
import re
import smtplib
import socket
import ssl
from dataclasses import dataclass
from email.headerregistry import Address
from typing import TYPE_CHECKING

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        GetMessageOperation,
        MoveMessageOperation,
        ReplyToMessageOperation,
        SearchMessagesOperation,
        SendMessageOperation,
        SetFlagsOperation,
    )

_HOST_LABEL = re.compile(r"[A-Za-z0-9_](?:[A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?\Z")


def is_server_host_name(host: str) -> bool:
    """Accept Go's DNS labels or an IP literal, without a URL or port."""
    if not host or len(host) > 253 or host.strip() != host:
        return False
    try:
        import ipaddress

        ipaddress.ip_address(host)
        return True
    except ValueError:
        pass
    return all(_HOST_LABEL.fullmatch(label) for label in host.removesuffix(".").split("."))


def is_bare_email_address(value: str) -> bool:
    """Check the ASCII addr-spec accepted by the Go connector."""
    if not value or len(value) > 254 or any(ord(c) > 127 or c.isspace() or ord(c) < 32 for c in value):
        return False
    try:
        address = Address(addr_spec=value)
    except Exception:
        return False
    if address.addr_spec != value or value.count("@") != 1:
        return False
    local, domain = value.rsplit("@", 1)
    return bool(local and "." in domain and is_server_host_name(domain))


def _has_control(value: str) -> bool:
    return any(ord(c) < 32 or 127 <= ord(c) <= 159 or c in "\u2028\u2029" for c in value)


def _failure(kind: FailureKind, operation: str, message: str) -> Failure:
    return Failure(kind, "email", operation, message)


@dataclass(frozen=True, slots=True)
class MailFailure(Exception):
    """Secret-safe protocol classification for one operation attempt."""

    outcome: str
    failure: Failure


class Client:
    """Open one verified TLS connection per operation and resolve credentials each time."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        tls_context: ssl.SSLContext | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("email configuration and credential provider are required")
        for protocol, host, port, security in (
            ("IMAP", config.imap_host, config.imap_port, config.imap_security),
            ("SMTP", config.smtp_host, config.smtp_port, config.smtp_security),
        ):
            if not is_server_host_name(host):
                raise ValueError(
                    f"email {protocol} host must be a host name or IP address without a scheme or port"
                )
            if type(port) is not int or not 1 <= port <= 65535:
                raise ValueError(f"email {protocol} port must be from 1 through 65535")
            if security not in ("implicitTLS", "startTLS"):
                raise ValueError(f"email {protocol} security must be implicitTLS or startTLS")
        if config.from_address and not is_bare_email_address(config.from_address):
            raise ValueError("email fromAddress must be one bare ASCII address")
        if config.from_name and (
            config.from_name.strip() != config.from_name
            or len(config.from_name) > 128
            or _has_control(config.from_name)
        ):
            raise ValueError(
                "email fromName must be at most 128 characters without surrounding spaces or line breaks"
            )
        self.config = config
        self.credentials = credentials
        if tls_context is not None and (
            tls_context.verify_mode != ssl.CERT_REQUIRED or not tls_context.check_hostname
            or tls_context.minimum_version < ssl.TLSVersion.TLSv1_2
        ):
            raise ValueError(
                "email TLS context must verify certificates and host names with TLS 1.2 or later"
            )
        self.tls_context = tls_context or ssl.create_default_context()
        # Generated project bindings pass this HTTP-only optional argument; SMTP and IMAP never use it.
        self._unused_http_client = http_client

    async def aclose(self) -> None:
        """No pooled transport survives an operation."""

    async def resolve_credentials(self, call: Call, operation: str) -> Credentials:
        """Get current credentials and reject unusable material before connecting."""
        try:
            credentials = await self.credentials.resolve(call)
            values = (
                credentials.username,
                credentials.password.reveal(),
                credentials.smtp_username,
                credentials.smtp_password.reveal(),
            )
            if not values[0] or not values[1] or any(_has_control(value) for value in values):
                raise ValueError("invalid credentials")
            return credentials
        except Exception as error:
            raise MailFailure(
                "defect",
                _failure(FailureKind.AUTHENTICATION, operation, "connection credentials are unavailable"),
            ) from error

    def sender(self, credentials: Credentials, operation: str) -> str:
        """Use the configured sender or a complete mailbox login."""
        sender = self.config.from_address or credentials.username
        if not is_bare_email_address(sender):
            raise MailFailure(
                "defect",
                _failure(
                    FailureKind.VALIDATION,
                    operation,
                    "fromAddress is blank and username is not a complete address, so the sender is unknown",
                ),
            )
        return sender

    def open_imap(self, credentials: Credentials, operation: str) -> imaplib.IMAP4:
        """Connect and authenticate only after certificate-verified TLS."""
        config = self.config
        session: imaplib.IMAP4
        try:
            if config.imap_security == "implicitTLS":
                session = imaplib.IMAP4_SSL(
                    config.imap_host, config.imap_port, ssl_context=self.tls_context, timeout=25
                )
            else:
                session = imaplib.IMAP4(config.imap_host, config.imap_port, timeout=25)
                try:
                    if "STARTTLS" not in session.capabilities:
                        raise MailFailure(
                            "rejected",
                            _failure(
                                FailureKind.PROTOCOL,
                                operation,
                                "the IMAP server did not accept STARTTLS, so the connector did not log in",
                            ),
                        )
                    session.starttls(self.tls_context)
                except Exception:
                    session.shutdown()
                    raise
            if "LOGINDISABLED" in session.capabilities:
                if "AUTH=PLAIN" not in session.capabilities:
                    raise MailFailure(
                        "rejected",
                        _failure(
                            FailureKind.AUTHENTICATION,
                            operation,
                            "the IMAP server disables LOGIN and offers no AUTH=PLAIN "
                            "over this TLS connection",
                        ),
                    )
                session.authenticate(
                    "PLAIN",
                    lambda _: ("\0" + credentials.username + "\0" + credentials.password.reveal()).encode(),
                )
            else:
                session.login(credentials.username, credentials.password.reveal())
            return session
        except MailFailure:
            raise
        except Exception as error:
            raise classify_mail_error(error, operation, "IMAP", "LOGIN") from error

    def open_smtp(self, credentials: Credentials, operation: str) -> smtplib.SMTP:
        """Connect, require TLS, then authenticate with the selected SMTP login."""
        config = self.config
        session: smtplib.SMTP
        try:
            if config.smtp_security == "implicitTLS":
                session = smtplib.SMTP_SSL(
                    config.smtp_host, config.smtp_port, timeout=15, context=self.tls_context
                )
            else:
                session = smtplib.SMTP(config.smtp_host, config.smtp_port, timeout=15)
                if (smtp_socket := getattr(session, "sock", None)) is not None:
                    smtp_socket.settimeout(30)
                code, response = session.ehlo("localhost")
                if code != 250:
                    raise classify_mail_error(
                        smtplib.SMTPResponseException(code, response), operation, "SMTP", "EHLO"
                    )
                if not session.has_extn("starttls"):
                    raise MailFailure(
                        "rejected",
                        _failure(
                            FailureKind.PROTOCOL,
                            operation,
                            "the SMTP server does not offer STARTTLS, so the connector did not authenticate",
                        ),
                    )
                if (smtp_socket := getattr(session, "sock", None)) is not None:
                    smtp_socket.settimeout(15)
                session.starttls(context=self.tls_context)
            if (smtp_socket := getattr(session, "sock", None)) is not None:
                smtp_socket.settimeout(30)
            code, response = session.ehlo("localhost")
            if code != 250:
                raise classify_mail_error(
                    smtplib.SMTPResponseException(code, response), operation, "SMTP", "EHLO"
                )
            if not isinstance(session.sock, ssl.SSLSocket):
                raise MailFailure(
                    "rejected",
                    _failure(
                        FailureKind.PROTOCOL,
                        operation,
                        "the SMTP connection is not encrypted, so the connector did not authenticate",
                    ),
                )
            auth = session.esmtp_features.get("auth", "").lower().split()
            if not ("plain" in auth or "login" in auth):
                raise MailFailure(
                    "rejected",
                    _failure(
                        FailureKind.AUTHENTICATION,
                        operation,
                        "the SMTP server offers neither AUTH PLAIN nor AUTH LOGIN over this TLS connection",
                    ),
                )
            username = credentials.smtp_username or credentials.username
            password = credentials.smtp_password.reveal() or credentials.password.reveal()
            session.esmtp_features["auth"] = "PLAIN" if "plain" in auth else "LOGIN"
            session.login(username, password)
            return session
        except MailFailure:
            raise
        except Exception as error:
            raise classify_mail_error(error, operation, "SMTP", "AUTH") from error

    def search_messages(self) -> "SearchMessagesOperation":
        """Return the IMAP search Query."""
        from .operations import SearchMessagesOperation

        return SearchMessagesOperation(self)

    def get_message(self) -> "GetMessageOperation":
        """Return the IMAP read Query."""
        from .operations import GetMessageOperation

        return GetMessageOperation(self)

    def send_message(self) -> "SendMessageOperation":
        """Return the SMTP send Mutation."""
        from .operations import SendMessageOperation

        return SendMessageOperation(self)

    def reply_to_message(self) -> "ReplyToMessageOperation":
        """Return the threaded SMTP reply Mutation."""
        from .operations import ReplyToMessageOperation

        return ReplyToMessageOperation(self)

    def move_message(self) -> "MoveMessageOperation":
        """Return the IMAP move Mutation."""
        from .operations import MoveMessageOperation

        return MoveMessageOperation(self)

    def set_flags(self) -> "SetFlagsOperation":
        """Return the absolute IMAP flag Mutation."""
        from .operations import SetFlagsOperation

        return SetFlagsOperation(self)


def classify_mail_error(error: Exception, operation: str, protocol: str, command: str) -> MailFailure:
    """Separate transport loss, authentication refusal, and malformed replies."""
    if isinstance(error, socket.gaierror) and error.errno == socket.EAI_NONAME:
        return MailFailure(
            "rejected",
            _failure(FailureKind.VALIDATION, operation, f"the {protocol} host name does not resolve"),
        )
    if isinstance(error, (ssl.SSLCertVerificationError, ssl.CertificateError)):
        return MailFailure(
            "rejected",
            _failure(
                FailureKind.PROTOCOL,
                operation,
                f"the {protocol} server certificate failed verification for the configured host",
            ),
        )
    if isinstance(error, ssl.SSLEOFError):
        return MailFailure(
            "retry", _failure(FailureKind.TRANSPORT, operation, f"the {protocol} TLS connection was lost")
        )
    if isinstance(error, ssl.SSLError):
        return MailFailure(
            "rejected", _failure(FailureKind.PROTOCOL, operation, f"the {protocol} server refused TLS")
        )
    if isinstance(error, smtplib.SMTPRecipientsRefused):
        first_response = next(iter(error.recipients.values()), None)
        if first_response is not None:
            return classify_mail_error(
                smtplib.SMTPResponseException(first_response[0], b""), operation, protocol, command
            )
    if isinstance(error, smtplib.SMTPResponseException):
        code = error.smtp_code
        if code // 100 == 4:
            kind = FailureKind.AVAILABILITY
            outcome = "retry"
        elif code in (535, 534, 530, 538):
            kind = FailureKind.AUTHENTICATION
            outcome = "rejected"
        elif code == 552:
            kind = FailureKind.RESPONSE_TOO_LARGE
            outcome = "rejected"
        else:
            kind = FailureKind.PROVIDER_REJECTION
            outcome = "rejected"
        return MailFailure(
            outcome, _failure(kind, operation, f"the SMTP server rejected {command} with {code}")
        )
    if isinstance(error, imaplib.IMAP4.abort):
        return MailFailure(
            "retry",
            _failure(FailureKind.TRANSPORT, operation, f"the IMAP connection was lost during {command}"),
        )
    if isinstance(error, imaplib.IMAP4.error):
        response = str(error)
        code_match = re.search(r"\[([A-Z]+)\]", response)
        response_code = code_match.group(1) if code_match else ""
        if command == "SELECT" and response_code in ("NONEXISTENT", "TRYCREATE"):
            return MailFailure(
                "not_found", _failure(FailureKind.NOT_FOUND, operation, "the mailbox does not exist")
            )
        if response_code in ("UNAVAILABLE", "INUSE", "SERVERBUG", "LIMIT"):
            kind = FailureKind.RATE_LIMIT if response_code == "LIMIT" else FailureKind.AVAILABILITY
            return MailFailure(
                "retry",
                _failure(kind, operation, f"the IMAP server answered {command} with NO [{response_code}]"),
            )
        if response_code in ("AUTHENTICATIONFAILED", "EXPIRED", "PRIVACYREQUIRED", "CONTACTADMIN"):
            kind = FailureKind.AUTHENTICATION
        elif response_code in ("AUTHORIZATIONFAILED", "NOPERM"):
            kind = FailureKind.AUTHORIZATION
        elif response_code in ("NONEXISTENT", "TRYCREATE"):
            kind = FailureKind.NOT_FOUND
        elif response_code == "OVERQUOTA":
            kind = FailureKind.QUOTA_EXHAUSTED
        elif "BAD" in response:
            kind = FailureKind.PROTOCOL
        else:
            kind = FailureKind.AUTHENTICATION if command == "LOGIN" else FailureKind.PROVIDER_REJECTION
        return MailFailure(
            "rejected", _failure(kind, operation, f"the IMAP server answered {command} with NO")
        )
    if isinstance(
        error,
        (
            socket.timeout,
            TimeoutError,
            ConnectionError,
            OSError,
            smtplib.SMTPServerDisconnected,
            imaplib.IMAP4.abort,
        ),
    ):
        return MailFailure(
            "retry",
            _failure(
                FailureKind.TRANSPORT, operation, f"the {protocol} connection was lost during {command}"
            ),
        )
    return MailFailure(
        "invalid",
        _failure(
            FailureKind.PROTOCOL,
            operation,
            f"the {protocol} server answered {command} with a response the connector cannot parse",
        ),
    )
