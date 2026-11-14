# vivo App Store connector

`GetOnlineVersion` reads the published Android version name and code for one package. The official [vivo App Store self-update API 2.0 guide](https://swsdl.vivo.com.cn/appstore/developer/uploadFile/20241224/vxpbua/vivo%E5%BC%80%E6%94%BE%E5%B9%B3%E5%8F%B0%E5%95%86%E5%BA%97%E8%87%AA%E6%9B%B4%E6%96%B02.0.pdf) defines the signed `POST /gateway/developer/app/publish/v1/get-online-version` read request, response fields and error codes. The connector returns only package and version metadata.

## Authorization

Sign in to [vivo Developers](https://developers.vivo.com/), open Account Information, and copy the developer account and its assigned HMAC secret into Dex Web Connections. The connector signs account, compact `bizParam`, and current millisecond timestamp with HMAC-SHA256. Leave timeout and maximum response size blank for displayed defaults.

## Example and verification

The [runnable Flow](go/examples/get-online-version/) starts with a Java package name and completes with typed `GetOnlineVersionOutput`. Its default mock returns a published version. Live vivo authorization, app publication and provider response variation remain unverified without a user-controlled Dex Web Connection.
