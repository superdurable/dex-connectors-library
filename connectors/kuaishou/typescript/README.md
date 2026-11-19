# Kuaishou TypeScript connector

`@superdurable/connector-kuaishou` provides the read-only `listUserVideos` Dex Step for one page of the authorized user's video metadata. It returns bounded photo IDs, captions, timestamps, counts, pending state, and a cursor while omitting cover and playback URLs.

The [runnable Flow and Worker](examples/videos/) use the static `kuaishou` connection. At the [Kuaishou Open Platform](https://open.kuaishou.com), open 控制台 > 应用管理, select an app with `user_video_info` permission, and enter its app ID and OAuth access token in Dex Web Connections. A blank cursor starts the first page. The endpoint, timeout, response limit, and page size have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client with a local provider that returns 503 before succeeding. Real Kuaishou authorization and video listing remain unverified without a user-controlled app and token.
