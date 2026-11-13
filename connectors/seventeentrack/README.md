# 17TRACK registered shipment statuses

This Go connector lists already registered tracking numbers and their package and tracking statuses through the [17TRACK Tracking API v2.4](https://api.17track.net/zh-cn/doc). It deliberately excludes contact details, postal codes, event text, and raw provider payloads. [17TRACK's privacy policy](https://www.17track.net/privacy-zh-cn.html) identifies 深圳市帝盟网络科技有限公司 as the China mainland operating entity.

`ListTrackedShipments` calls `POST https://api.17track.net/track/v2.4/gettracklist` with `17token` in the protected request header and a one-based page number in JSON. The [official contract](https://api.17track.net/zh-cn/doc#%E8%8E%B7%E5%8F%96%E5%88%97%E8%A1%A8) defines 40 records per page and a limit of three requests per second. The operation reads shipments already registered in the account; registering a new number is a separate billed action and is not included. HTTP 429, 5xx, and transport failures retry; definite authorization errors fail the Flow.

The [runnable Dex Flow](go/examples/tracked-shipments/README.md) shows setup, pagination, and typed output. The logo uses the [official 17TRACK SVG](https://www.17track.net/assets/logo-full.svg). Live authorization and production responses remain unverified.
