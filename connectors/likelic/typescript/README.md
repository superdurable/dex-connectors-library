# Likelic TypeScript connector

`@superdurable/connector-likelic` provides the read-only `listRooms` Dex Step. It returns one page of room identifiers and push states, omitting group names, company details, and member counts.

The [runnable Flow and Worker](examples/rooms/) use the static `likelic` connection. In Dex Web Connections, create an API key at [Likelic Agent > 外部拓展 > API 设置](https://agent.likelic.com) and grant 推送通知 at 系统设置 > 开放 API 权限. Enter the `sk-` key and customer code from 企业设置. The timeout and response limit have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client with a local provider that returns 503 before succeeding. Real Likelic authorization and room listing remain unverified without user-controlled credentials.
