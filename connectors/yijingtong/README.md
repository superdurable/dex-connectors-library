# Yijingtong warehouse catalog

This Go connector lists warehouse codes, names, and countries authorized for a Yijingtong overseas warehouse tenant. It excludes inventory, addresses, and customer information. [Yijingtong](https://www.kjwlxt.com/about_us.php) is operated by the Chinese company 广州市甲壳虫数字科技有限公司.

`ListWarehouses` sends a JSON `POST` to `/hwc_api/hwc_storage_list.php` at the tenant's assigned HTTPS production origin, with the secret token in the request body. The [official warehouse contract](https://szy.kjwlxt.com/hwc_openapi_docs/?page=storage_list) defines the request and response; the [onboarding guide](https://szy.kjwlxt.com/hwc_openapi_docs/?page=overview) says customer service supplies the production domain and token. The documentation host serves a sandbox and is not a production default. HTTP 429, 5xx, and transport failures retry; definite provider rejection fails the Flow.

The [runnable Dex Flow](go/examples/warehouse-list/README.md) shows setup and typed output. The logo embeds the [official Yijingtong site logo](https://www.kjwlxt.com/themes/default/images/logo.png) unchanged. Live authorization and production warehouse responses remain unverified.
