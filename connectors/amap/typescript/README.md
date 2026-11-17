# Amap TypeScript connector

`@superdurable/connector-amap` provides the read-only `geocodeAddress` Query. It converts a structured Chinese address into zero or more Amap location candidates. The result contains Amap coordinates and no API Key. The request uses [Amap Web Service geocoding](https://lbs.amap.com/api/webservice/guide/api/georegeo) through HTTPS.

Create a Web Service Key in [Amap Application Management](https://console.amap.com/dev/key/app), then save it as the secret `api_key` in the `amap` Dex Web Connection. The manifest supplies the public HTTPS origin and bounded request defaults. Amap sends the Key in the required `key` query parameter; request URLs and transport errors do not enter Flow results or logs. Apply Amap's map-coordinate usage rules to returned locations.

The [runnable address lookup Flow](examples/address-lookup/README.md) includes Worker and Client entrypoints. It wires the `geocoded` branch; provider rejection, invalid response, and local defect fail the Flow. HTTP 408, 429, 5xx, transport failures, and Amap rate-limit infocodes retry. The Go-recorded conformance scenarios and real Dex integration test use a local fake provider. A live Amap call through a user-controlled Dex Web Connection remains unverified.

## Verification

```bash
npm ci
npm run check
```
