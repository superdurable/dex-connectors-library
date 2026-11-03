# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the email example against a TLS IMAP stand-in and real Dex stack."""

import asyncio
import os
import socketserver
import ssl
from datetime import UTC, datetime, timedelta
from ipaddress import ip_address
from pathlib import Path
from threading import Thread
from typing import cast
from uuid import uuid4

import dex
import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from dex_connectors.sdk import ConnectionRef, MutationResult, SecretString, StaticCredentialProvider

from dex_connectors.superdurable_email import (
    Client,
    Config,
    Connection,
    Credentials,
    SearchMessagesInput,
    SearchMessagesOutput,
    SendMessageInput,
    SentMessage,
    new_connection,
    new_send_message_step,
)

from .flow import CONNECTION_NAME, MailboxSearchFlow

_MESSAGE = (
    b"From: Jane <jane@example.com>\r\n"
    b"To: agent@example.com\r\n"
    b"Subject: Invoice\r\n"
    b"Message-ID: <invoice@example.com>\r\n"
    b"Date: Tue, 03 Nov 2026 18:00:00 +0000\r\n"
    b"Content-Type: text/plain; charset=utf-8\r\n\r\nInvoice body\r\n"
)


class IMAPHandler(socketserver.StreamRequestHandler):
    """Serve the commands used by the search example over verified TLS."""

    def handle(self) -> None:
        """Respond to CAPABILITY, LOGIN, EXAMINE, UID SEARCH/FETCH, and LOGOUT."""
        self.wfile.write(b"* OK email stand-in ready\r\n")
        while line := self.rfile.readline():
            fields = line.decode("ascii", errors="replace").strip().split(" ", 2)
            if len(fields) < 2:
                return
            tag, command = fields[:2]
            args = fields[2] if len(fields) == 3 else ""
            cast(TLSIMAPServer, self.server).commands.append((command, args))
            if command == "CAPABILITY":
                self.wfile.write(b"* CAPABILITY IMAP4rev1\r\n")
                answer = "OK capabilities"
            elif command == "LOGIN":
                answer = "OK logged in"
            elif command == "EXAMINE":
                self.wfile.write(b"* 1 EXISTS\r\n* OK [UIDVALIDITY 42] stable\r\n")
                answer = "OK [READ-ONLY] selected"
            elif command == "UID" and args.startswith("SEARCH"):
                self.wfile.write(b"* SEARCH 97\r\n")
                answer = "OK searched"
            elif command == "UID" and args.startswith("FETCH"):
                headers = _MESSAGE.split(b"\r\n\r\n", 1)[0] + b"\r\n\r\n"
                metadata = (
                    b'1 FETCH (UID 97 FLAGS () INTERNALDATE "03-Nov-2026 18:01:00 +0000" '
                    b'RFC822.SIZE 222 BODYSTRUCTURE ("TEXT" "PLAIN" ("CHARSET" "utf-8") '
                    b'NIL NIL "7BIT" 12 1 NIL NIL NIL NIL) BODY[HEADER] {'
                    + str(len(headers)).encode()
                    + b"}\r\n"
                )
                self.wfile.write(b"* " + metadata + headers + b")\r\n")
                answer = "OK fetched"
            elif command == "LOGOUT":
                self.wfile.write(b"* BYE done\r\n")
                answer = "OK logout"
            else:
                answer = "BAD unsupported"
            self.wfile.write(f"{tag} {answer}\r\n".encode())
            self.wfile.flush()
            if command == "LOGOUT":
                return


class TLSIMAPServer(socketserver.ThreadingTCPServer):
    """Listen on an OS-selected port and encrypt every connection."""

    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, tls_context: ssl.SSLContext) -> None:
        self.tls_context = tls_context
        self.commands: list[tuple[str, str]] = []
        super().__init__(("127.0.0.1", 0), IMAPHandler)

    def get_request(self) -> tuple[ssl.SSLSocket, tuple[str, int]]:
        """Complete TLS before IMAP receives any command."""
        raw_socket, address = super().get_request()
        return self.tls_context.wrap_socket(raw_socket, server_side=True), address


def create_certificate(directory: Path) -> tuple[Path, Path]:
    """Create a local CA certificate with a loopback IP subject alternate name."""
    private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    subject = x509.Name([x509.NameAttribute(x509.NameOID.COMMON_NAME, "127.0.0.1")])
    now = datetime.now(UTC)
    certificate = (
        x509.CertificateBuilder()
        .subject_name(subject)
        .issuer_name(subject)
        .public_key(private_key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - timedelta(minutes=1))
        .not_valid_after(now + timedelta(hours=1))
        .add_extension(x509.SubjectAlternativeName([x509.IPAddress(ip_address("127.0.0.1"))]), critical=False)
        .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
        .sign(private_key, hashes.SHA256())
    )
    certificate_path = directory / "mail.crt"
    key_path = directory / "mail.key"
    certificate_path.write_bytes(certificate.public_bytes(serialization.Encoding.PEM))
    key_path.write_bytes(
        private_key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.TraditionalOpenSSL,
            serialization.NoEncryption(),
        )
    )
    return certificate_path, key_path


@pytest.mark.asyncio
async def test_mailbox_search_through_real_dex_and_verified_imap(tmp_path: Path) -> None:
    """Confirm Flow completion and the provider's UID summary across Worker and Client."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    certificate_path, key_path = create_certificate(tmp_path)
    server_context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    server_context.load_cert_chain(str(certificate_path), str(key_path))
    provider = TLSIMAPServer(server_context)
    provider_thread = Thread(target=provider.serve_forever, daemon=True)
    provider_thread.start()
    trusted_context = ssl.create_default_context(cafile=str(certificate_path))
    reference = ConnectionRef("email", CONNECTION_NAME)
    credentials = StaticCredentialProvider(
        {reference: Credentials("agent@example.com", SecretString("inert"), "", SecretString(""))}
    )
    client = Client(
        Config(
            "127.0.0.1", "127.0.0.1", "", "", provider.server_address[1], "implicitTLS", 35802, "implicitTLS"
        ),
        credentials,
        tls_context=trusted_context,
    )
    connection = new_connection(client, reference)
    flow = MailboxSearchFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as dex_client:
            flow_id = "email-python-search-" + uuid4().hex
            await dex_client.start_flow(flow, flow_id, SearchMessagesInput(subject_contains="Invoice"))
            result = await asyncio.wait_for(dex_client.wait_for_flow(flow_id, timedelta(seconds=30)), 40)
            assert result.status == dex.FlowStatus.COMPLETED, (result.error_message, provider.commands)
            output = result.single_output(SearchMessagesOutput)
            assert output.messages[0].message_id == "invoice@example.com"
            assert output.messages[0].reference.uid == 97
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await connection.aclose()
        provider.shutdown()
        provider.server_close()
        provider_thread.join(timeout=5)


def map_to_send_input(value: SendMessageInput) -> SendMessageInput:
    """Pass the safe send input to the connector Mutation."""
    return value


# dex:group group-id:email-send-probe group-label:"Send probe"
# dex:explanation text:"Finish after SMTP accepts a message."
class CompleteSendProbe(dex.Step[MutationResult[SentMessage]]):
    """Complete a test Flow after confirmed SMTP acceptance."""

    def get_step_type(self) -> str:
        """Return one stable completion Step identity."""
        return "CompleteEmailSendProbe"

    def execute(self, context: dex.Context, input: MutationResult[SentMessage]) -> dex.StepDecision:
        """Return the stable Message-ID without a credential."""
        return dex.graceful_complete(input.value)


class SendProbeFlow(dex.Flow[SendMessageInput]):
    """Exercise the email heartbeat claim through a real Dex Worker."""

    def __init__(self, connection: Connection) -> None:
        self.complete = CompleteSendProbe()
        self.send = new_send_message_step(
            step_type="SendEmailProbe",
            step_input_type=SendMessageInput,
            annotations={
                "groupId": "email-send-probe",
                "groupLabel": "Send probe",
                "explanation": "Submit one email after a durable checkpoint.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_send_input,
            sent=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable test Flow identity."""
        return "EmailSendProbePython"

    def get_steps(self) -> dex.StepList[SendMessageInput]:
        """Register the send and completion Steps."""
        return dex.StepList.start_step(self.send).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist no Attributes or Streams."""
        return dex.PersistenceSchema.of()


@pytest.mark.asyncio
async def test_send_retry_and_heartbeat_through_real_dex(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """A temporary SMTP refusal retries once and then completes with one accepted message."""
    from tests.test_send_message import SMTPServer

    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    reference = ConnectionRef("email", CONNECTION_NAME)
    credentials = StaticCredentialProvider(
        {reference: Credentials("agent@example.com", SecretString("inert"), "", SecretString(""))}
    )
    client = Client(
        Config("127.0.0.1", "127.0.0.1", "", "", 35801, "implicitTLS", 35802, "implicitTLS"), credentials
    )
    sessions: list[SMTPServer] = []

    def open_smtp(*args: object) -> SMTPServer:
        session = SMTPServer(final_code=451 if not sessions else 250)
        sessions.append(session)
        return session

    monkeypatch.setattr(client, "open_smtp", open_smtp)
    connection = new_connection(client, reference)
    flow = SendProbeFlow(connection)
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache = dex.open_blob_cache(dex.BlobCacheConfig(str(tmp_path / "blobs"), 1 << 24))
    worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
    worker_task = asyncio.create_task(worker.start())
    try:
        await worker.wait_pull_registered(timeout=30)
        async with dex.AsyncClient(
            registry, cache, dex.ClientOptions(server_address=server_address)
        ) as dex_client:
            flow_id = "email-python-send-" + uuid4().hex
            await dex_client.start_flow(
                flow, flow_id, SendMessageInput(to=("jane@example.com",), subject="Probe", text="Hello")
            )
            result = await asyncio.wait_for(dex_client.wait_for_flow(flow_id, timedelta(seconds=30)), 40)
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            sent = result.single_output(SentMessage)
            assert sent.message_id.startswith("dex-")
            assert len(sessions) == 2
            assert all(session.commands.count("MAIL") == 1 for session in sessions)
    finally:
        await worker.stop()
        await worker_task
        cache.close()
        await connection.aclose()
