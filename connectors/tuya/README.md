# Tuya Cloud connector

This Go connector reads safe device metadata with Tuya's [Get device details](https://developer.tuya.com/en/docs/cloud/device-management?id=K9g6rfntdz78a) endpoint, `GET /v1.0/devices/{device_id}`. One `getDeviceDetails` Query first obtains a simple-mode access token from `GET /v1.0/token?grant_type=1`, then signs the device request using the current [HMAC-SHA256 scheme](https://developer.tuya.com/en/docs/iot/new-singnature?id=Kbw0q34cs2e5g). It requests a fresh token on each attempt. The device response includes `local_key`; the connector explicitly discards that secret and all status values, returning only ID, name, connectivity, category, product ID, and creation/update timestamps.

## Connection

At [Tuya Developer Platform](https://platform.tuya.com/), open **Cloud > Development > My Cloud Projects > Open Project > Overview**. Copy Access ID and Access Secret from **Authorization Key** into Dex credential fields `client_id` and `client_secret`. Confirm the project has **IoT Core** and **Authorization Token Management** API services and is linked to the target device. Select the endpoint for the project's **Data Center**; China is the manifest default. The project secret is used only to sign requests and never sent to Tuya. A different data center cannot access the project's devices.

Find the device ID under **Cloud > Development > My Cloud Projects > Open Project > Devices > All Devices**. The ID is the Flow input, not a credential. Tuya's endpoint list and request headers are documented in [Request Structure](https://developer.tuya.com/en/docs/iot/api-request?id=Ka4a8uuo1j4t4).

## Go

The [device details example](go/examples/device-details/) runs a Worker with static Dex Web connection name `tuya`. Its Flow calls `tuya.NewGetDeviceDetailsStep` and completes on `found`. `notFound`, `providerRejected`, `invalidResponse`, and `defect` are optional branches; an unwired branch fails the Flow. Transient transport, rate limit, HTTP 408/5xx, and Tuya system/token-expiry codes retry under the default Query policy.

No live Tuya credentials are bundled. The mock example exercises the real Dex Worker and Server; a live provider call requires your own linked cloud project and connection.
