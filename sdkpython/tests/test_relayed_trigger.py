# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Relay signatures, broker registration, and relayed request verification with token rotation."""

import asyncio
import hashlib
import hmac
import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import httpx
import pytest

from dex_connectors.sdk.projectconfig import (
    ConfigurationStore,
    ConnectionKey,
    ConnectionNotSelectedError,
    ConnectionStore,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    InMemoryObjectStore,
    LoadedProject,
    RelayedTriggerNotRelayedError,
    RelaySignatureInvalidError,
    RelayTokenRenewalDeferredError,
    Scope,
    SnapshotRef,
    TriggerInboxKey,
)
from dex_connectors.sdk.projectconfig.broker import CredentialBrokerClient
from dex_connectors.sdk.projectconfig.models import (
    ApplicationSecretRef,
    Configuration,
    ConnectionConfiguration,
    CredentialBrokerConfiguration,
)
from dex_connectors.sdk.relaysignature import (
    RELAY_SIGNATURE_HEADER,
    RELAY_TIMESTAMP_HEADER,
    ParsedRelaySignature,
    RelaySignatureRefusal,
    parse_relay_signature,
    sign_relayed_request,
    verify_relay_signature,
)

BODY = b'{"event_id":"Ev1"}'
SIGNED_AT = datetime.fromtimestamp(1_760_227_200, UTC)
SCOPE = Scope("test", "live")
ENDPOINT = "https://broker.example.test/internal/connection-credentials/read"


def _headers(token: str, signed_at: datetime = SIGNED_AT) -> dict[str, str]:
    return {
        RELAY_TIMESTAMP_HEADER.lower(): str(int(signed_at.timestamp())),
        RELAY_SIGNATURE_HEADER.lower(): sign_relayed_request(token, signed_at, BODY),
    }


def test_signs_the_shared_vector_and_returns_typed_refusals() -> None:
    expected = hmac.new(b"scope-token", b"v1:1760227200:" + BODY, hashlib.sha256).hexdigest()
    signature = sign_relayed_request("scope-token", SIGNED_AT, BODY)
    assert signature == f"v1={expected}"
    assert signature == "v1=b94d74f33919d2a8390208d6274f78e61574baa906f03f74d58465e2c4e96bf7"
    headers = _headers("scope-token")
    valid = verify_relay_signature("scope-token", headers, BODY, SIGNED_AT + timedelta(minutes=1))
    assert valid.is_valid and valid.signed_at == SIGNED_AT
    assert verify_relay_signature("scope-token", list(headers.items()), BODY, SIGNED_AT).is_valid
    upper = {RELAY_TIMESTAMP_HEADER: "1760227200", RELAY_SIGNATURE_HEADER: signature}
    assert verify_relay_signature("scope-token", upper, BODY, SIGNED_AT).is_valid
    refusals = {
        "other token": verify_relay_signature("other-token", headers, BODY, SIGNED_AT),
        "other body": verify_relay_signature("scope-token", headers, b'{"event_id":"Ev2"}', SIGNED_AT),
        "missing": verify_relay_signature(
            "scope-token", {RELAY_TIMESTAMP_HEADER: "1760227200"}, BODY, SIGNED_AT
        ),
        "stale": verify_relay_signature("scope-token", headers, BODY, SIGNED_AT + timedelta(minutes=6)),
    }
    assert {name: result.refusal for name, result in refusals.items()} == {
        "other token": RelaySignatureRefusal.MISMATCH,
        "other body": RelaySignatureRefusal.MISMATCH,
        "missing": RelaySignatureRefusal.MISSING,
        "stale": RelaySignatureRefusal.STALE,
    }


def test_refuses_malformed_and_out_of_range_timestamps_without_errors() -> None:
    signature = sign_relayed_request("scope-token", SIGNED_AT, BODY)
    for timestamp, header in (
        ("+1760227200", signature),
        ("1" * 16, signature),
        ("1760227200\n", signature),
        ("1760227200", "v2=" + "0" * 64),
        ("1760227200", "v1=" + "0" * 63),
        ("1760227200", "v1=" + "g" * 64),
    ):
        parsed = parse_relay_signature(
            {RELAY_TIMESTAMP_HEADER: timestamp, RELAY_SIGNATURE_HEADER: header}, SIGNED_AT
        )
        assert parsed is RelaySignatureRefusal.MALFORMED, (timestamp, header)
    for timestamp in ("999999999999999", "-999999999999999"):
        refused = parse_relay_signature(
            {RELAY_TIMESTAMP_HEADER: timestamp, RELAY_SIGNATURE_HEADER: signature}, SIGNED_AT
        )
        assert refused is RelaySignatureRefusal.STALE
    for offset, is_valid in ((300, True), (-300, True), (301, False), (-301, False)):
        parsed = parse_relay_signature(_headers("t"), SIGNED_AT + timedelta(seconds=offset))
        assert isinstance(parsed, ParsedRelaySignature) is is_valid, offset
    upper_hex = {
        RELAY_TIMESTAMP_HEADER: "1760227200",
        RELAY_SIGNATURE_HEADER: signature.upper().replace("V1=", "v1="),
    }
    assert verify_relay_signature("scope-token", upper_hex, BODY, SIGNED_AT).is_valid
    with pytest.raises(ValueError):
        parse_relay_signature(_headers("t"), datetime(2025, 1, 1))


def _reference(version: str) -> ApplicationSecretRef:
    return ApplicationSecretRef(
        f"projects/test/live/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/{'a' * 32}",
        version,
        "sha256:" + "b" * 64,
    )


class TokenVault:
    """Secret versions by reference; current_reference names the newest version."""

    def __init__(self, tokens: dict[str, str], current: str) -> None:
        self.tokens = tokens
        self.current = current
        self.reads: list[str] = []
        self.read_delay = 0.0

    async def read_token(self, reference: ApplicationSecretRef) -> str:
        self.reads.append(reference.version)
        if self.read_delay:
            await asyncio.sleep(self.read_delay)
        return self.tokens[reference.version]

    async def current_reference(self) -> ApplicationSecretRef:
        return _reference(self.current)


def _broker(
    vault: TokenVault,
    answer: Callable[[httpx.Request], httpx.Response],
    clock: Callable[[], float] | None = None,
) -> tuple[CredentialBrokerClient, list[httpx.Request]]:
    requests: list[httpx.Request] = []

    def record(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return answer(request)

    client = httpx.AsyncClient(transport=httpx.MockTransport(record), follow_redirects=False)
    broker = CredentialBrokerClient(
        CredentialBrokerConfiguration(ENDPOINT, _reference("v1")),
        vault.read_token,
        vault.current_reference,
        client,
        clock=clock,
    )
    return broker, requests


def _project(broker: CredentialBrokerClient) -> LoadedProject:
    store = InMemoryObjectStore()
    configuration = Configuration(
        SCOPE,
        1,
        (
            ConnectionConfiguration(
                "slack", "support", "slack", "slack", {}, shared_connection_id="conn-slack"
            ),
            ConnectionConfiguration("slack", "private", "slack", "slack", {}),
        ),
    )
    return LoadedProject(
        configuration,
        SnapshotRef("k", "1", "sha256:" + "0" * 64),
        ConnectionStore(store, SCOPE),
        ConfigurationStore(store, SCOPE),
        broker=broker,
    )


async def test_registers_on_the_broker_origin_and_maps_every_answer() -> None:
    statuses = ["REGISTERED", "CONNECTION_NOT_SELECTED", "NOT_RELAYED", "SURPRISE"]
    vault = TokenVault({"v1": "scope-token"}, "v1")
    broker, requests = _broker(vault, lambda request: httpx.Response(200, json={"status": statuses.pop(0)}))
    project = _project(broker)
    support = ConnectionKey("slack", "support")
    path = "/internal/slack-triggers/register"
    await project.register_relayed_trigger(support, path)
    assert str(requests[0].url) == "https://broker.example.test/internal/slack-triggers/register"
    assert requests[0].headers["authorization"] == "Bearer scope-token"
    assert json.loads(requests[0].content) == {
        "sharedConnectionId": "conn-slack",
        "connectionName": "support",
    }
    assert list(json.loads(requests[0].content)) == ["sharedConnectionId", "connectionName"]
    for expected in (
        ConnectionNotSelectedError,
        RelayedTriggerNotRelayedError,
        CredentialBrokerRejectedError,
    ):
        with pytest.raises(expected):
            await project.register_relayed_trigger(support, path)
    with pytest.raises(ValueError, match="shared connection entry for slack/private"):
        await project.register_relayed_trigger(ConnectionKey("slack", "private"), path)
    for invalid in ("https://elsewhere.test/x", "//elsewhere.test/x", "/x?y", "/x#y", "/x\\y", "relative"):
        with pytest.raises(ValueError, match="absolute path"):
            await project.register_relayed_trigger(support, invalid)
    unavailable, _ = _broker(vault, lambda request: httpx.Response(503))
    with pytest.raises(CredentialBrokerUnavailableError):
        await _project(unavailable).register_relayed_trigger(support, path)


async def test_registration_renews_the_token_once_after_401() -> None:
    vault = TokenVault({"v1": "old-token", "v2": "new-token"}, "v2")
    broker, requests = _broker(
        vault,
        lambda request: httpx.Response(
            401 if request.headers["authorization"] == "Bearer old-token" else 200,
            json={"status": "REGISTERED"},
        ),
    )
    await broker.register_relayed_trigger("conn-slack", "support", "/internal/slack-triggers/register")
    assert [request.headers["authorization"] for request in requests] == [
        "Bearer old-token",
        "Bearer new-token",
    ]


async def test_mismatch_rereads_a_rotated_version_at_most_every_30_seconds() -> None:
    now = [1000.0]
    vault = TokenVault({"v1": "old-token", "v2": "rotated-token"}, "v1")
    broker, _ = _broker(vault, lambda request: httpx.Response(200, json={}), clock=lambda: now[0])
    project = _project(broker)
    received = datetime.now(UTC)
    await project.verify_relayed_request(_headers("old-token", received), BODY, received)
    with pytest.raises(RelaySignatureInvalidError):
        await project.verify_relayed_request({}, BODY, received)
    assert vault.reads == ["v1"]
    vault.current = "v2"
    await project.verify_relayed_request(_headers("rotated-token", received), BODY, received)
    # The rotation is a new secret version, so the reference was resolved again.
    assert vault.reads == ["v1", "v2"]
    for _ in range(2):
        with pytest.raises(RelayTokenRenewalDeferredError):
            await project.verify_relayed_request(_headers("forged-token", received), BODY, received)
    assert vault.reads == ["v1", "v2"]
    now[0] += 30.0
    with pytest.raises(RelaySignatureInvalidError):
        await project.verify_relayed_request(_headers("forged-token", received), BODY, received)
    assert vault.reads == ["v1", "v2", "v2"]


async def test_concurrent_forged_requests_share_one_reread_and_rotation_is_accepted_later() -> None:
    now = [1000.0]
    vault = TokenVault({"v1": "old-token", "v2": "rotated-token"}, "v1")
    vault.read_delay = 0.005
    broker, _ = _broker(vault, lambda request: httpx.Response(200, json={}), clock=lambda: now[0])
    received = datetime.now(UTC)
    results = await asyncio.gather(
        *(
            broker.verify_relayed_request(_headers("forged-token", received), BODY, received)
            for _ in range(20)
        ),
        return_exceptions=True,
    )
    assert all(isinstance(result, Exception) for result in results)
    assert len(vault.reads) == 2
    vault.current = "v2"
    with pytest.raises(RelayTokenRenewalDeferredError):
        await broker.verify_relayed_request(_headers("rotated-token", received), BODY, received)
    now[0] += 30.0
    await broker.verify_relayed_request(_headers("rotated-token", received), BODY, received)
    assert len(vault.reads) == 3
    stale = {RELAY_TIMESTAMP_HEADER: "1", RELAY_SIGNATURE_HEADER: sign_relayed_request("x", SIGNED_AT, BODY)}
    with pytest.raises(RelaySignatureInvalidError):
        await broker.verify_relayed_request(stale, BODY, received)
    assert len(vault.reads) == 3


async def test_cached_token_serves_reads_while_a_reread_hangs() -> None:
    vault = TokenVault({"v1": "scope-token"}, "v1")
    is_hanging = asyncio.Event()

    async def hanging_reference() -> ApplicationSecretRef:
        is_hanging.set()
        await asyncio.Event().wait()
        raise AssertionError("unreachable")

    ready = {"status": "READY", "credential": {"accessToken": "private"}, "revision": 1}
    client = httpx.AsyncClient(transport=httpx.MockTransport(lambda request: httpx.Response(200, json=ready)))
    broker = CredentialBrokerClient(
        CredentialBrokerConfiguration(ENDPOINT, _reference("v1")), vault.read_token, hanging_reference, client
    )
    received = datetime.now(UTC)
    await broker.verify_relayed_request(_headers("scope-token", received), BODY, received)
    forged = asyncio.ensure_future(
        broker.verify_relayed_request(_headers("forged-token", received), BODY, received)
    )
    await asyncio.wait_for(is_hanging.wait(), 5)
    credential = await asyncio.wait_for(broker.read("host-selection"), 1)
    assert credential.credential == {"accessToken": "private"}
    await asyncio.wait_for(
        broker.verify_relayed_request(_headers("scope-token", received), BODY, received), 1
    )
    forged.cancel()


async def test_unreadable_tokens_are_unavailable_and_projects_need_a_broker() -> None:
    async def failing(reference: ApplicationSecretRef) -> str:
        raise OSError("SENTINEL token store unavailable")

    vault = TokenVault({}, "v1")
    broker = CredentialBrokerClient(
        CredentialBrokerConfiguration(ENDPOINT, _reference("v1")),
        failing,
        vault.current_reference,
        httpx.AsyncClient(transport=httpx.MockTransport(lambda request: httpx.Response(200))),
    )
    received = datetime.now(UTC)
    with pytest.raises(CredentialBrokerUnavailableError) as raised:
        await broker.verify_relayed_request(_headers("old-token", received), BODY, received)
    assert "SENTINEL" not in str(raised.value)
    project = _project(broker)
    project.broker = None
    with pytest.raises(ValueError, match="relayed Trigger verification requires a credential broker"):
        await project.verify_relayed_request({}, BODY, received)
    with pytest.raises(ValueError, match="relayed Trigger registration requires a credential broker"):
        await project.register_relayed_trigger(ConnectionKey("slack", "support"), "/x")
    assert project.shared_connection_id(ConnectionKey("slack", "support")) == "conn-slack"
    assert project.shared_connection_id(ConnectionKey("slack", "private")) is None
    inbox = project.trigger_inbox(TriggerInboxKey("slack", "support", "threadReplyCreated", "reply"))
    assert inbox.key.startswith("projects/test/live/trigger-inboxes/slack/")
