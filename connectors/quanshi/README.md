# Quanshi connector

[Quanshi](https://www.quanshi.com/about) is operated by 全时云商务服务股份有限公司. The official [service-domain guide](https://developer.quanshi.com/cn/api/560) gives the current production and beta HTTPS OpenAPI origins. Its [token creation API](https://developer.quanshi.com/cn/api/69) signs `AppSecret + Nonce + CurTime` with lowercase SHA-1 hex; the token response supplies an administrator `userId`. The [meeting room list API](https://developer.quanshi.com/cn/api/170) is read-only POST. The room-list page still shows the older `openapi.quanshi.com` host; this connector uses the current production host from the service-domain guide with the documented room-list path. It returns only room ID, name, capacity, and numeric room status. Room phone, equipment, and manager details are omitted.

## Connection

Register or sign in through the [Quanshi developer platform](https://developer.quanshi.com/cn) and request a developer key for Cloud Meeting OpenAPI. Enter its AppId and AppSecret in the secret connection fields. Ask Quanshi support to enable meeting room list access if needed. Leave `baseUrl` blank for production or set the documented beta HTTPS origin when testing; the connector rejects all other hosts and HTTP. Leave timeout and response-size fields blank for displayed defaults.

`listRooms` takes no user input. It obtains a short-lived token on each call, derives the administrator `userId` from the token response, then lists rooms. The token and `userId` are not returned to the Flow. Optional `providerRejected`, `invalidResponse`, and `defect` branches remain unwired in the example. HTTP 429, 408, 5xx, and transport failures retry. Documented token-expired and cross-tenant codes are classified as authentication and authorization failures.

## Example

The [room directory Flow](go/examples/room-directory/) is a runnable Worker with static Dex connection name `quanshi`. Its mock is synthetic and verified with a real Dex Worker and Client. Live Quanshi authorization, current-domain compatibility of the older room-list path, and room status behavior remain unverified without an approved account.
