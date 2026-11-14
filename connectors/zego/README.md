# ZEGO connector

This connector calls the official [ZIM QueryRoomUserList API](https://doc-zh.zego.im/zim-server/room/obtain-information-about-users-in-a-room) at `GET https://zim-api.zego.im/?Action=QueryRoomUserList` to read the current room `Count`. It requests at most one user record (`Limit=1`) and discards that record, `RequestId`, and provider messages. ZEGO's [server API guide](https://doc-zh.zego.im/zim-server/accessing-server-apis) documents the signed request, credential path, and signature test vector.

In [ZEGO Console](https://console.zego.im/), open 控制台 > 项目管理 for AppId, then click 查看 > 项目配置 for the matching ServerSecret. Save both in Dex Web Connections. Each request uses a fresh 8-byte random nonce and Unix timestamp; the signature is lowercase hex MD5 of `AppId + SignatureNonce + ServerSecret + Timestamp`. The secret and signed URL are not returned, persisted, or logged. Redirects are disabled.

Run the [example Flow](go/examples/get-room-user-count/) with the generated mock or a user-controlled connection. Room IDs are assigned by the application and ZEGO limits this API to 20 calls per second. A missing or destroyed room is a terminal provider rejection.
