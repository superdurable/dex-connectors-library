# HONOR App Market connector

`LookupAppID` reads the numeric ID for an Android package owned by the connected HONOR developer account. The official [Publish API guide](https://developer.honor.com/cn/doc/guides/101359) documents the client credentials token exchange, `GET /openapi/v1/publish/get-app-id`, package-name input, and `code` / `data[].appId` response. The connector returns only the package name and app ID.

## Authorization

Sign in to [HONOR Developers](https://developer.honor.com/), open Management Center > Open Capabilities > Credentials, request an API key, and save its Client ID and paired API key as secret Dex Web Connection fields. Create an Android app in App Management and bind its package name. The connector exchanges the credentials for a short-lived Bearer token per request and never places the token in Flow results, receipts, or logs. Leave timeout and maximum response size blank for their displayed defaults.

## Example and verification

The [runnable Flow](go/examples/lookup-app-id/) starts with `{"packageName":"com.example.app"}` and completes with a typed `LookupAppIDOutput`. Its default mock returns one app ID. Live HONOR authorization, app ownership, and provider response variation remain unverified without a user-controlled Dex Web Connection.
