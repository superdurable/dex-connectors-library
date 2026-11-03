# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider address checks and a DNS-pinned httpcore network backend."""

from __future__ import annotations

import ipaddress
import socket
from collections.abc import Iterable
from typing import Any, Literal

import anyio
import httpcore
import httpx

AddressPolicy = Literal["publicHTTPS", "privateNetworkHTTP", "unrestricted"]
_SocketOption = (
    tuple[int, int, int]
    | tuple[int, int, bytes | bytearray]
    | tuple[int, int, None, int]
)

_PRIVATE_NETWORKS = tuple(
    ipaddress.ip_network(cidr)
    for cidr in ("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7")
)
_SHARED_NETWORK = ipaddress.ip_network("100.64.0.0/10")
_METADATA_IPV6 = ipaddress.ip_address("fd00:ec2::254")
_BROADCAST_IPV4 = ipaddress.ip_address("255.255.255.255")


class AddressRefusal(ValueError):
    """A policy refusal before dialing; its reason contains no host or credential."""

    def __init__(self, reason: str) -> None:
        self.reason = reason
        super().__init__(f"address not allowed: {reason}")


def _checked_ip(address: str) -> ipaddress.IPv4Address | ipaddress.IPv6Address:
    parsed = ipaddress.ip_address(address)
    if isinstance(parsed, ipaddress.IPv6Address) and parsed.ipv4_mapped is not None:
        return parsed.ipv4_mapped
    return parsed


def _refusal_reason(address: str, scheme: str, policy: AddressPolicy) -> str:
    try:
        parsed = _checked_ip(address)
    except ValueError:
        return "resolved address is invalid"
    is_private = parsed.is_loopback or any(parsed in network for network in _PRIVATE_NETWORKS)
    if parsed.is_link_local or parsed == _METADATA_IPV6:
        return "link-local addresses, which include the cloud metadata address, are never allowed"
    if parsed.is_unspecified or parsed.is_multicast or parsed == _BROADCAST_IPV4:
        return "only unicast addresses are allowed"
    if policy == "publicHTTPS" and scheme != "https":
        return "plain http is not allowed; use https"
    if policy == "publicHTTPS" and (is_private or parsed in _SHARED_NETWORK):
        return "only public addresses are allowed"
    if policy == "privateNetworkHTTP" and scheme == "http" and not is_private:
        return "plain http is allowed only when every resolved address is loopback or private; use https"
    return ""


async def _resolve_addresses(host: str, port: int) -> list[str]:
    try:
        return [str(_checked_ip(host))]
    except ValueError:
        pass
    try:
        answers = await anyio.getaddrinfo(host, port, type=socket.SOCK_STREAM)
    except OSError:
        raise httpcore.ConnectError("provider address resolution failed") from None
    addresses = [answer[4][0] for answer in answers]
    if not addresses:
        raise httpcore.ConnectError("provider address resolution returned no addresses")
    return addresses


class _AddressPolicyBackend(httpcore.AsyncNetworkBackend):
    """Resolve and check every address, then connect to a checked IP.

    httpcore retains the original request origin for Host, TLS SNI, and certificate
    verification. The backend owns each DNS lookup and never hands a hostname to
    its underlying TCP connector. Callers close the owning HTTP client.
    """

    def __init__(self, policy: AddressPolicy, scheme: str) -> None:
        if policy not in ("publicHTTPS", "privateNetworkHTTP"):
            raise ValueError("address policy is invalid")
        self.policy = policy
        self.scheme = scheme
        self._network_backend = httpcore.AnyIOBackend()

    async def connect_tcp(
        self,
        host: str,
        port: int,
        timeout: float | None = None,
        local_address: str | None = None,
        socket_options: Iterable[_SocketOption] | None = None,
    ) -> httpcore.AsyncNetworkStream:
        """Dial only after every DNS answer passes the selected policy."""
        addresses = await _resolve_addresses(host, port)
        checked = []
        for address in addresses:
            reason = _refusal_reason(address, self.scheme, self.policy)
            if reason:
                raise AddressRefusal(reason)
            checked.append(str(_checked_ip(address)))
        last_error: httpcore.ConnectError | httpcore.ConnectTimeout | None = None
        for address in checked:
            try:
                return await self._network_backend.connect_tcp(
                    address, port, timeout=timeout, local_address=local_address,
                    socket_options=socket_options,
                )
            except (httpcore.ConnectError, httpcore.ConnectTimeout) as error:
                last_error = error
        assert last_error is not None
        raise last_error

    async def connect_unix_socket(
        self, path: str, timeout: float | None = None,
        socket_options: Iterable[_SocketOption] | None = None,
    ) -> httpcore.AsyncNetworkStream:
        """Refuse Unix sockets because they bypass the checked DNS address."""
        raise AddressRefusal("Unix sockets are not allowed")

    async def sleep(self, seconds: float) -> None:
        """Wait between httpcore connection retries."""
        await self._network_backend.sleep(seconds)


class _AddressPolicyTransport(httpx.AsyncBaseTransport):
    def __init__(
        self, policy: AddressPolicy, *, verify: Any = True, cert: Any = None,
        limits: httpx.Limits | None = None, http1: bool = True, http2: bool = False,
    ) -> None:
        limits = limits or httpx.Limits()
        self._plain = httpx.AsyncHTTPTransport(
            verify=verify, cert=cert, trust_env=False, limits=limits, http1=http1, http2=http2
        )
        self._secure = httpx.AsyncHTTPTransport(
            verify=verify, cert=cert, trust_env=False, limits=limits, http1=http1, http2=http2
        )
        for transport, scheme in ((self._plain, "http"), (self._secure, "https")):
            # httpx lacks a backend constructor; the fresh pool still owns the original URL origin for TLS.
            pool = transport._pool
            pool._network_backend = _AddressPolicyBackend(policy, scheme)

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        if request.url.scheme == "http":
            return await self._plain.handle_async_request(request)
        if request.url.scheme == "https":
            return await self._secure.handle_async_request(request)
        raise AddressRefusal("URL scheme is not allowed")

    async def aclose(self) -> None:
        await self._plain.aclose()
        await self._secure.aclose()
