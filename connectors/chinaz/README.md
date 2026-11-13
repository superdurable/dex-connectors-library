# Chinaz site TDK metadata

This Go connector reads public website title, keywords, description, and IP for one domain through the [Chinaz Site TDK Information API](https://api.chinaz.com/ApiDetails/SiteData). Chinaz is operated in China by 厦门享联科技有限公司, as its official [company page](https://www.chinaz.com/aboutus/index.html) identifies.

`GetSiteTDK` sends `POST https://apidatav2.chinaz.com/single/tdk` with a multipart form containing `key` and `domain`. The secret key is kept out of the URL, errors, receipts, and Flow state. The [official API contract](https://api.chinaz.com/ApiDetails/SiteData) defines the HTTPS POST option, response `StateCode` and `Result`, plan pricing, and errors. Calls consume paid allowance. `10025` means no data; key or plan errors are terminal; frequency limits and temporary provider errors retry.

The [runnable Dex Flow](go/examples/site-tdk/README.md) shows setup and typed output. The logo embeds the unchanged [official Chinaz API logo](https://csstools.chinaz.com/tools/images/public/logos/logo-api.png). Live authorization and production TDK results remain unverified.
