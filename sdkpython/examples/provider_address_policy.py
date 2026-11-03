# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise private-network provider HTTP against an inert local endpoint."""

import asyncio

from dex_connectors.sdk.providerhttp import AddressRefusal, new_provider_http_client


async def main() -> None:
    """Send to loopback and verify that the public policy refuses it."""

    async def respond(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        await reader.readuntil(b"\r\n\r\n")
        writer.write(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
        await writer.drain()
        writer.close()
        await writer.wait_closed()

    server = await asyncio.start_server(respond, "127.0.0.1", 0)
    port = server.sockets[0].getsockname()[1]
    try:
        url = f"http://127.0.0.1:{port}/"
        async with new_provider_http_client(address_policy="privateNetworkHTTP") as client:
            response = await client.get(url)
            assert response.text == "ok"
        async with new_provider_http_client(address_policy="publicHTTPS") as client:
            try:
                await client.get(url)
            except AddressRefusal:
                pass
            else:
                raise AssertionError("publicHTTPS accepted a loopback address")
    finally:
        server.close()
        await server.wait_closed()


if __name__ == "__main__":
    asyncio.run(main())
