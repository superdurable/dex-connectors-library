# Huawei Cloud IAM TypeScript connector

List projects visible to the connected Huawei Cloud IAM user without persisting the password or token.

Install `@superdurable/connector-huaweicloud` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `listAccessibleProjects` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
