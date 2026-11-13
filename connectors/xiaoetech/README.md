# XiaoeTech (小鹅通) connector

This Go connector calls the official [root goods category API](https://api-doc.xiaoe-tech.com/api_list/product/get_goods_new_category_class.html) at `POST https://api.xiaoe-tech.com/xe.goods.category.list/2.0.0` with `parent_id: 0`. It returns only category IDs, titles, and child availability, omitting nested details and merchant data. The endpoint requires the **Product Management - Query** permission set and has a published limit of 1,000 calls per 10 seconds.

## Connection

Sign in to the [Cloud Service Console](https://admin.cloud.xiaoe-tech.com/) from the store admin at **Marketing Center > API Self Development**. Create an application, have a store administrator approve its authorization, and request the Product Management - Query permission set. In Application Information, copy the store `app_id`, application `client_id`, and secret `secret_key` into the Dex connection. Add the Dex Worker outbound IP to the console's IP allowlist. The connector obtains a 2-hour `access_token` with the [official token API](https://api-doc.xiaoe-tech.com/develop_guide/get_access_token.html) and caches it until five minutes before expiry. It refreshes after an expired-token response. The provider requires `secret_key` in the token GET query; the connector does not log that URL or include its credentials/token in results. Leave base URL, timeout, and response limit blank for displayed defaults.

The [Open API pricing notice](https://api-doc.xiaoe-tech.com/xiaoe_cloud_charge.html) ties access and billing to the store edition. This read-only operation retries transient failures at most three times because API usage may be billed.

## Go

The [root goods categories Flow](go/examples/root-goods-categories/) is a runnable Worker with static Dex Web connection name `xiaoetech`. `listed` completes the Flow; optional branches fail it unless an application wires a business response. The mock integration test uses synthetic sample data and a real Dex Worker and Client. Live authorization, billing, and category data remain unverified without a user-controlled connection.
