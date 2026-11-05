# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider selection, address policy, and text generation Query binding."""

import ipaddress
import re
import socket
from urllib.parse import urlsplit

import anyio
import httpx
from dex_connectors.sdk import Call, CredentialProvider, SecretString
from dex_connectors.sdk.providerhttp import AddressRefusal, validate_base_url
from dex_connectors.sdk.textgen import TextGenerationQuery, TextGenerationQueryConfig

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GENERATE_TEXT_DEFINITION
from .chat_profiles import new_chat_wire_format

_PROVIDERS: dict[str, tuple[str, dict[str, str]]] = {
    "openai": ("gpt-6-sol", {"global": "https://api.openai.com/v1"}),
    "openai-compatible": ("", {"global": ""}),
    "anthropic": ("claude-sonnet-5", {"global": "https://api.anthropic.com"}),
    "gemini": ("gemini-3.5-flash-lite", {"global": "https://generativelanguage.googleapis.com/v1beta"}),
    "qwen": (
        "qwen3.7-plus",
        {
            "global": "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
            "hong-kong": "https://cn-hongkong.dashscope.aliyuncs.com/compatible-mode/v1",
            "china": "https://dashscope.aliyuncs.com/compatible-mode/v1",
        },
    ),
    "deepseek": ("deepseek-flash", {"global": "https://api.deepseek.com"}),
    "meta": ("muse-spark-1.3", {"global": "https://api.meta.ai"}),
    "mistral": (
        "mistral-large-2512",
        {
            "global": "https://api.mistral.ai/v1",
            "eu": "https://api.eu.mistral.ai/v1",
            "us": "https://api.us.mistral.ai/v1",
        },
    ),
    "kimi": ("kimi-k2.6", {"global": "https://api.moonshot.ai/v1", "china": "https://api.moonshot.cn/v1"}),
    "xai": ("grok-4.3", {"global": "https://api.x.ai/v1", "us": "https://us.api.x.ai/v1"}),
}


def _loopback_host(host: str) -> bool:
    if host.lower() == "localhost":
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


async def _check_loopback(host: str) -> None:
    if not _loopback_host(host):
        raise AddressRefusal("provider address is not loopback")
    if host.lower() != "localhost":
        return
    try:
        addresses = await anyio.getaddrinfo(host, None, type=socket.SOCK_STREAM)
    except OSError as error:
        raise AddressRefusal("provider address could not be resolved") from error
    if not addresses or any(
        not isinstance(result[4][0], str) or not _loopback_host(result[4][0]) for result in addresses
    ):
        raise AddressRefusal("provider address is not loopback")


class _LoopbackTransport(httpx.AsyncBaseTransport):
    def __init__(self) -> None:
        self._transport = httpx.AsyncHTTPTransport()

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        await _check_loopback(str(request.url.host))
        return await self._transport.handle_async_request(request)

    async def aclose(self) -> None:
        await self._transport.aclose()


class Client:
    """A connection's provider-neutral generateText Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        base_url_for_test: str = "",
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("llm configuration and credentials are required")
        provider = config.provider.strip()
        if provider not in _PROVIDERS:
            raise ValueError("llm configuration provider is invalid")
        default_model, urls = _PROVIDERS[provider]
        region = config.region or "global"
        if region not in urls:
            raise ValueError("llm provider does not serve configured region")
        if config.max_response_bytes < 0:
            raise ValueError("llm maxResponseBytes must be nonnegative")
        model = config.model.strip()
        if provider == "openai-compatible":
            if not model:
                raise ValueError("llm openai-compatible model is required")
            base_url = validate_base_url(config.base_url.strip())
            if not urlsplit(base_url).path.endswith("/v1"):
                raise ValueError("llm openai-compatible baseUrl must end in /v1")
            host = urlsplit(base_url).hostname or ""
            try:
                address = ipaddress.ip_address(host)
            except ValueError:
                pass
            else:
                if isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped:
                    address = address.ipv4_mapped
                if not (address.is_loopback or address.is_global):
                    raise ValueError("base URL host must be public or loopback")
        else:
            if config.base_url:
                raise ValueError("llm baseUrl applies only to openai-compatible")
            base_url = urls[region]
        workspace = config.anthropic_workspace_id.strip()
        if workspace and provider != "anthropic":
            raise ValueError("llm anthropicWorkspaceId applies only to anthropic")
        if workspace and not re.fullmatch(r"wrkspc_[A-Za-z0-9]{1,128}", workspace):
            raise ValueError("llm anthropicWorkspaceId must be a workspace ID")
        if base_url_for_test:
            base_url = validate_base_url(base_url_for_test)
            if not _loopback_host(urlsplit(base_url).hostname or ""):
                raise ValueError("llm test base URL must use a loopback host")
        is_loopback = _loopback_host(urlsplit(base_url).hostname or "")
        if http_client is not None and (not is_loopback or not base_url_for_test):
            raise ValueError("llm custom HTTP client requires a loopback test URL")
        self._owns_http_client = http_client is None and is_loopback
        request_timeout_seconds = 1170 if provider == "deepseek" else 870
        self._http_client = http_client or (
            httpx.AsyncClient(
                transport=_LoopbackTransport(), follow_redirects=False, timeout=request_timeout_seconds
            ) if is_loopback else None
        )
        if provider == "openai":
            from .responses_wire_format import new_responses_wire_format

            wire_format = new_responses_wire_format()
        elif provider == "anthropic":
            from .claude_wire_format import new_claude_wire_format

            wire_format = new_claude_wire_format(workspace)
        elif provider == "gemini":
            from .gemini_wire_format import new_gemini_wire_format

            wire_format = new_gemini_wire_format()
        else:
            wire_format = new_chat_wire_format(provider)

        async def resolve_credential(call: Call) -> SecretString:
            resolved = await credentials.resolve(call)
            return resolved.api_key

        self._generate_text = TextGenerationQuery(
            TextGenerationQueryConfig(
                definition=GENERATE_TEXT_DEFINITION,
                wire_format=wire_format,
                base_url=base_url,
                connection_model=model or default_model,
                request_timeout_seconds=request_timeout_seconds,
                resolve_credential=resolve_credential,
                max_response_bytes=config.max_response_bytes
                or (64 << 20 if provider == "deepseek" else 8 << 20),
                max_stream_event_bytes=1 << 20,
                http_client=self._http_client,
                address_policy="unrestricted" if is_loopback else "publicHTTPS",
            )
        )

    def generate_text(self) -> TextGenerationQuery:
        """Return the operation bound to this connection."""
        return self._generate_text

    async def aclose(self) -> None:
        """Close a connector-owned loopback provider client."""
        if self._owns_http_client and self._http_client is not None:
            await self._http_client.aclose()
