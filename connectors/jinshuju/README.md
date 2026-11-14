# Jinshuju connector

This connector reads a page of [Jinshuju forms](https://open.jinshuju.net/api_v1/endpoints/get_forms/) through `GET https://jinshuju.net/api/v1/forms`. It returns only form names, form tokens, total/count, and the next-page cursor. The provider response also includes creator names, tags, and timestamps; this connector omits them and never requests submissions.

Jinshuju API v1 uses an Access Token in `Authorization: Bearer <token>`. Create a personal token at **avatar > 个人中心 > API > 个人 Access Token > 创建 Token**, or an enterprise-wide token at **系统设置 > 企业 API > 企业 Access Token > 创建 Token** if your enterprise plan needs all forms. The token is shown once; copy it into Dex Web Connections without `Bearer`. The [official authentication guide](https://open.jinshuju.net/api_v1/authentication/) explains scope and revocation. The [runnable Flow](go/examples/list-forms-page/README.md) uses the static `jinshuju` connection.

Real Access Token authorization and Dex Web form interaction remain unverified.
