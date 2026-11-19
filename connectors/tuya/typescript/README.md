# Tuya TypeScript connector

`@superdurable/connector-tuya` implements the `getDeviceDetails` Query. In the [Tuya IoT Platform](https://platform.tuya.com/), open **Cloud → Development → My Cloud Projects → Open Project → Overview** to find the project data center, client ID, and client secret. Select the corresponding fixed `endpoint` and enter the ID and secret as private credentials in Dex Connections. Find a linked device ID under the project **Devices** tab. Blank timeout and response limit retain the manifest defaults.

The connector obtains a simple-mode token, signs a device GET, and returns only safe metadata; project credentials, access token, `local_key`, and device status data stay outside Flow state. Temporary HTTP/transport failures and Tuya system codes retry. Not found, provider refusal, invalid response, and local defects select optional branches that fail the Flow unless wired.

`examples/basic/` includes a runnable Flow, Worker, and Client. `npm run check` replays exact Go-signed scenarios. Run `npm run test:integration` with `dexcli dev -open=false` for real Dex 503 retry and persistence verification. Live Tuya authorization remains unverified without user-controlled project credentials.
