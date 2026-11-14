# TiankeChat (天科云客服)

TiankeChat is operated by 河南骁兔文化传媒有限公司. Its [official developer guide](https://www.tiankechat.cn/developer.html) documents the HTTPS Open API, v3 HMAC-SHA256 authorization, and `GET /api/openapi/knowledge`. This connector reads one page of knowledge entry metadata: IDs, categories, enabled flags, and hit counts. It excludes questions, answers, attachments, and customer details.

## Connection

In your TiankeChat admin console at [chat.open-code.cn](https://chat.open-code.cn/), open **系统 > 开放平台 > 应用**. Create or select an application and copy its `app_id` (`ap_` for production or `sb_` for sandbox) and one-time-shown `app_secret` (`tks_`). The connector signs each request; it never transmits the secret. An older v2-only application needs a coordinated secret reset to use v3; resetting revokes its previous credentials. For a private deployment, set `baseUrl` to its HTTPS origin, without `/api`. The public HTTPS origin, timeout, and response size have defaults in Dex Web.

The `listKnowledgeEntries` Query accepts a one-based page number and page size from 1 to 100. Zero values use page 1 and 20 items. Only the `listed` branch is a successful result. Authorization and protocol failures select optional branches; transient failures retry. The API guide documents a server maximum of 500 items, but this connector caps requests at 100 to bound Flow results.

## Example

The [knowledge catalog Flow](go/examples/knowledge-catalog/) is a runnable Worker with static Dex connection name `tiankechat`. Its local mock uses synthetic data and can be verified through a real Dex Worker and Client. Live application authorization, deployment-specific HTTPS configuration, and provider pagination remain unverified until a user connects a TiankeChat tenant.
