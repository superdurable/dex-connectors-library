# WeCom Python connector

This package implements `sendGroupRobotMessage` and `sendApplicationMessage` as typed Dex Mutation Steps. Both use Dex heartbeat Fences to cap possible sends. The application send obtains and caches an access token and enables WeCom's four-hour duplicate check.

[Group robot](examples/group_robot/) and [application](examples/application/) contain runnable Flows, Worker entrypoints, and real Dex Worker/Client integration tests with synthetic WeCom HTTP responses. The [manifest](../connector.yaml) has the Dex Web connection setup instructions.

Run `uv run pytest tests/test_conformance.py` to replay Go-recorded provider requests, branches, retry decisions, and Fence heartbeats. Live WeCom authorization and delivery have not been verified.
