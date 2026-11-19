# JiHu GitLab TypeScript connector

`@superdurable/connector-jihugitlab` implements the read-only `listMemberProjects` Query. It returns one page of project IDs, names, paths and the next page. The personal access token stays in Dex Connections and does not enter Flow state.

Create a personal access token with `read_api` scope in your [JiHu GitLab profile](https://docs.gitlab.cn/docs/jh/user/profile/personal_access_tokens/) and configure it in Dex Connections using the [authorization guide](../connector.yaml). Leave the HTTPS origin, timeout and response limit blank for manifest defaults unless using a private deployment. HTTP 408/429/5xx and transport failures retry. Provider refusals, invalid responses and local defects select optional branches.

`examples/basic/` contains a runnable Dex Flow, Worker and Client. `npm run check` replays Go-recorded provider scenarios; `npm run test:integration` against `dexcli dev -open=false` checks retry and persistence. Live provider authorization remains unverified without a user-controlled connection.
