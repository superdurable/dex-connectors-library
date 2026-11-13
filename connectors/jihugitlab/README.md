# JiHu GitLab connector

[JiHu GitLab](https://gitlab.cn/) is operated by [极狐信息技术（湖北）有限公司](https://gitlab.cn/terms/statement). Its current [Projects API](https://docs.gitlab.cn/docs/jh/api/projects/) documents `GET /api/v4/projects` with `membership=true` and pagination. The [REST authentication guide](https://docs.gitlab.cn/docs/jh/api/rest/authentication/) documents the `PRIVATE-TOKEN` header. This connector lists one bounded page of projects joined by the token owner, returning only project ID, name, path with namespace, and next-page number. It does not return owners, email addresses, or repository contents.

## Connection

On [JihuLab.com](https://jihulab.com/) or your organization's HTTPS JiHu GitLab deployment, open **avatar > 编辑个人资料 > 访问 > 个人访问令牌**. Follow the [personal access token guide](https://docs.gitlab.cn/docs/jh/user/profile/personal_access_tokens/), create a token with `read_api` scope and an expiry, and enter the one-time token in the secret `api_key` field. Use an account with only the project memberships required by the Flow. Leave `baseUrl` blank for JihuLab.com. For a private deployment, set it to the HTTPS origin only, without `/api` or a path. The connector rejects HTTP and path-bearing origins. The timeout and response-size fields have safe Dex Web defaults.

`listMemberProjects` accepts one-based `page` and `perPage`; zero values use page 1 and 20 projects. Page size is capped at 100. The optional `providerRejected`, `invalidResponse`, and `defect` branches remain unwired in the example. Transient HTTP and transport failures retry; authorization failures do not.

## Example

The [project catalog Flow](go/examples/project-catalog/) is a runnable Worker with static Dex connection name `jihugitlab`. Its local mock uses synthetic project metadata and is verified with a real Dex Worker and Client. Live JihuLab authorization, private deployment compatibility, and provider pagination remain unverified until connected to an approved account.
