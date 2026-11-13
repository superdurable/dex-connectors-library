# Halo (FIT2CLOUD) connector

[Halo](https://www.halo.run/) is a CMS product of the Chinese software company [FIT2CLOUD 飞致云](https://fit2cloud.com/about/). Its [REST API introduction](https://docs.halo.run/developer-guide/restful-api/introduction) documents personal access token authentication. The [Halo OpenAPI reference](https://api.halo.run/) defines the `GET /apis/content.halo.run/v1alpha1/posts` endpoint and `PostList`, `Post`, and `PostSpec` response schemas. This connector targets Halo 2.27 and reads one bounded page of article metadata. It returns only article name, title, slug, publication state, visibility, and next-page number; it does not return article body, authors, or account details.

## Connection

On your organization's Halo site, enable HTTPS first. In the Halo user menu open **personal center > Personal Access Tokens**; follow the [user center guide](https://docs.halo.run/guide/use/user-center) to create a token with only article-read permission. Copy the `pat_` prefixed value into Dex secret `api_key`. The token has the permissions of its owner. Set `baseUrl` to the Halo site's HTTPS origin only, without `/apis` or any path. The connector rejects HTTP and path-bearing origins. Timeout and response-size fields have safe Dex Web defaults.

`listPosts` accepts one-based `page` and `pageSize`; zero values use page 1 and 20 articles. Page size is capped at 100. A valid `PostList` response selects `listed`; the optional failure branches are unwired in the example. Transient HTTP and transport failures retry. A private post is listed only if the token owner has read access to it.

## Example

The [post catalog Flow](go/examples/post-catalog/) is a runnable Worker with static Dex connection name `fit2cloud`. Its local mock uses synthetic article metadata and is verified with a real Dex Worker and Client. Live Halo token authorization, deployment version compatibility, and provider pagination remain unverified until connected to an approved site.
