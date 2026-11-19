# Polyv Python connector

`dex-connector-polyv` implements the signed `listChannels` Query. Configure the App ID and App Secret in Dex Web Connections using the [Polyv console](https://console.polyv.net). The Query returns only channel IDs.

See the [runnable channel-catalog Flow](examples/channel_catalog/) for Worker setup. Go-recorded conformance scenarios cover complete signed requests and decisions. The checked-in Dex integration test uses a fake provider; live Polyv authorization and retrieval remain unverified.
