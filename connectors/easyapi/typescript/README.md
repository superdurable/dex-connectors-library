# EasyAPI E-Invoice TypeScript connector

`@superdurable/connector-easyapi` implements the read-only `queryShop` Query for one authorized company's invoicing service configuration. Its result includes only `shopId`, `name`, `apiState`, and `endTime`. The tax number, credentials, address, bank, and certificate data stay out of Flow results.

In the [EasyAPI E-Invoice console](https://fapiao.easyapi.com/console/), open **服务认证**, create or select a key pair associated with the company, and save its Key and Secret as Dex secret credentials. Use the company's 15, 17, 18, 19, or 20 character tax number as Query input. Blank credentials prevent requests. Leave baseUrl, timeout and maxResponseBytes blank for their displayed production defaults.

The [runnable Flow](examples/basic/) uses static connection name `easyapi`. `npm run check` replays all Go-recorded provider scenarios, including rejection, retry, and secret-safe output. Live provider authorization remains unverified without a user-controlled connection.
