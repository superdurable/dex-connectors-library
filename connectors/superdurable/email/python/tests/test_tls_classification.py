# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Credential and transport classification for native protocol exchanges."""

import imaplib
import smtplib
import socket
import ssl

import pytest
from dex_connectors.sdk import FailureKind, SecretString

from dex_connectors.superdurable_email import Config, Credentials
from dex_connectors.superdurable_email.client import (
    Client,
    MailFailure,
    classify_mail_error,
    is_bare_email_address,
)

from .test_send_message import CredentialsProvider


class IMAPWithoutStartTLS:
    """Server that advertises no TLS upgrade."""

    capabilities = ("IMAP4REV1",)
    login_calls = 0

    def __init__(self, *args: object, **kwargs: object) -> None:
        """Accept a plaintext socket before any login."""

    def login(self, *args: object) -> None:
        """Make any leaked credential observable."""
        self.login_calls += 1

    def shutdown(self) -> None:
        """Close the refusal without authenticating."""


class SMTPWithoutStartTLS:
    """Server that advertises no TLS upgrade."""

    login_calls = 0

    def __init__(self, *args: object, **kwargs: object) -> None:
        """Accept a plaintext socket before any login."""

    def ehlo(self, *args: object) -> tuple[int, bytes]:
        """Advertise basic SMTP only."""
        return 250, b"hello"

    def has_extn(self, extension: str) -> bool:
        """Refuse STARTTLS."""
        return False

    def login(self, *args: object) -> None:
        """Make any leaked credential observable."""
        self.login_calls += 1


def test_imap_starttls_refusal_never_sends_credentials(monkeypatch: pytest.MonkeyPatch) -> None:
    """Missing STARTTLS is terminal before LOGIN or AUTHENTICATE."""
    server = IMAPWithoutStartTLS()
    monkeypatch.setattr(imaplib, "IMAP4", lambda *args, **kwargs: server)
    client = Client(
        Config("127.0.0.1", "127.0.0.1", "", "", 35801, "startTLS", 35802, "implicitTLS"),
        CredentialsProvider(),
    )
    credentials = Credentials("agent@example.com", SecretString("inert"), "", SecretString(""))
    with pytest.raises(MailFailure) as rejected:
        client.open_imap(credentials, "searchMessages")
    assert rejected.value.outcome == "rejected"
    assert server.login_calls == 0


def test_smtp_starttls_refusal_never_sends_credentials(monkeypatch: pytest.MonkeyPatch) -> None:
    """Missing STARTTLS is terminal before SMTP AUTH."""
    server = SMTPWithoutStartTLS()
    monkeypatch.setattr(smtplib, "SMTP", lambda *args, **kwargs: server)
    client = Client(
        Config("127.0.0.1", "127.0.0.1", "", "", 35801, "implicitTLS", 35802, "startTLS"),
        CredentialsProvider(),
    )
    credentials = Credentials("agent@example.com", SecretString("inert"), "", SecretString(""))
    with pytest.raises(MailFailure) as rejected:
        client.open_smtp(credentials, "sendMessage")
    assert rejected.value.outcome == "rejected"
    assert server.login_calls == 0


def test_tls_verification_and_dial_refusal_have_distinct_outcomes() -> None:
    """A bad certificate is terminal while a local refused dial retries."""
    certificate = classify_mail_error(
        ssl.SSLCertVerificationError("bad certificate"), "sendMessage", "SMTP", "TLS"
    )
    refused = classify_mail_error(ConnectionRefusedError("refused"), "sendMessage", "SMTP", "connect")
    unknown_host = classify_mail_error(
        socket.gaierror(socket.EAI_NONAME, "missing"), "searchMessages", "IMAP", "connect"
    )
    assert certificate.outcome == "rejected" and certificate.failure.kind == FailureKind.PROTOCOL
    assert refused.outcome == "retry" and refused.failure.kind == FailureKind.TRANSPORT
    assert unknown_host.outcome == "rejected" and unknown_host.failure.kind == FailureKind.VALIDATION


def test_smtp_status_classifies_all_temporary_and_credential_codes() -> None:
    """Every 4xx retries and credential 5xx ends in rejection."""
    for code in (421, 450, 451, 452, 499):
        assert (
            classify_mail_error(smtplib.SMTPDataError(code, b"inert"), "sendMessage", "SMTP", "DATA").outcome
            == "retry"
        )
    for code in (530, 534, 535, 538):
        failure = classify_mail_error(
            smtplib.SMTPAuthenticationError(code, b"inert"), "sendMessage", "SMTP", "AUTH"
        )
        assert failure.outcome == "rejected" and failure.failure.kind == FailureKind.AUTHENTICATION


def test_address_validation_matches_bare_ascii_mailbox_policy() -> None:
    """Reject malformed, non-ASCII, and domain-literal sender addresses."""
    assert is_bare_email_address("agent+tag@example.com")
    for invalid in (
        '"two words"@example.com',
        "agent..name@example.com",
        "agent@bad..example.com",
        "Jöhn@example.com",
        "agent@exämple.com",
        "agent@[127.0.0.1]",
        "Agent <agent@example.com>",
    ):
        assert not is_bare_email_address(invalid)


def test_imap_starttls_upgrades_before_login(monkeypatch: pytest.MonkeyPatch) -> None:
    """The IMAP credential is sent only after the STARTTLS upgrade."""

    class UpgradableIMAP:
        capabilities = ("IMAP4REV1", "STARTTLS")

        def __init__(self) -> None:
            self.upgraded = False
            self.login_count = 0

        def starttls(self, context: ssl.SSLContext) -> None:
            self.upgraded = True

        def login(self, username: str, password: str) -> tuple[str, list[bytes]]:
            assert self.upgraded
            self.login_count += 1
            return "OK", [b"logged in"]

    server = UpgradableIMAP()
    monkeypatch.setattr(imaplib, "IMAP4", lambda *args, **kwargs: server)
    client = Client(
        Config("127.0.0.1", "127.0.0.1", "", "", 35801, "startTLS", 35802, "implicitTLS"),
        CredentialsProvider(),
    )
    credentials = Credentials("agent@example.com", SecretString("inert"), "", SecretString(""))
    client.open_imap(credentials, "searchMessages")
    assert server.login_count == 1


def test_smtp_starttls_upgrades_before_authentication(monkeypatch: pytest.MonkeyPatch) -> None:
    """The SMTP credential is sent only after STARTTLS and verified TLS state."""

    class SecureSocket:
        pass

    class UpgradableSMTP:
        def __init__(self) -> None:
            self.sock: SecureSocket | None = None
            self.esmtp_features = {"auth": "PLAIN LOGIN"}
            self.login_count = 0

        def ehlo(self, *args: object) -> tuple[int, bytes]:
            return 250, b"hello"

        def has_extn(self, extension: str) -> bool:
            return extension == "starttls"

        def starttls(self, context: ssl.SSLContext) -> tuple[int, bytes]:
            self.sock = SecureSocket()
            return 220, b"upgraded"

        def login(self, username: str, password: str) -> tuple[int, bytes]:
            assert isinstance(self.sock, SecureSocket)
            self.login_count += 1
            return 235, b"authenticated"

    server = UpgradableSMTP()
    monkeypatch.setattr(smtplib, "SMTP", lambda *args, **kwargs: server)
    monkeypatch.setattr(ssl, "SSLSocket", SecureSocket)
    client = Client(
        Config("127.0.0.1", "127.0.0.1", "", "", 35801, "implicitTLS", 35802, "startTLS"),
        CredentialsProvider(),
    )
    credentials = Credentials("agent@example.com", SecretString("inert"), "", SecretString(""))
    client.open_smtp(credentials, "sendMessage")
    assert server.login_count == 1
