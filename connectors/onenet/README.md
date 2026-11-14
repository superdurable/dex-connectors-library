# OneNET AIoT connector

The Go `queryDeviceDetail` Query calls the current [OneNET AIoT device detail endpoint](https://iot.10086.cn/doc/aiot/aiot/detail/1466). It takes a device name and reads the device's product ID, name, status, description and timestamps. The provider's raw response can include device access keys, PSKs and other credentials; the connector discards those fields and never places them in Dex results or attributes. This connector targets the current AIoT API, not the legacy `api.heclouds.com/devices` product line.

## Configure Dex Web Connections

At [OneNET](https://iot.10086.cn), open personal **Account Information** for the main user ID and **Access Permissions** for the paired Base64 access key. Under **Product Development > Product List > Product Information**, copy the product ID. Enter these as `userId`, secret `accessKey`, and `productId`. The connector generates a one-hour SHA-256 [Authorization token](https://onenet.hk.chinamobile.com/doc/v5/fuse/detail/279) for every call. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for the displayed defaults.

The Query selects optional `defect` for invalid local inputs, `providerRejected` for definite provider errors, and `invalidResponse` for malformed or oversized JSON. HTTP 429, HTTP 5xx and transport failures retry. The example leaves optional branches unwired, so those outcomes fail the Flow.

See the runnable [`go/examples/device-read`](go/examples/device-read). Tests use a local HTTP provider and a real Dex server with the generated mock. Live OneNET authorization and device data retrieval remain unverified.
