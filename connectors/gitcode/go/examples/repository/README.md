# GitCode repository Flow

This runnable Flow uses the static `gitcode` connection to retrieve one repository's identity and visibility. The `fetched` branch stores the result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Run from this Go module:

```sh
go run ./examples/repository
```

In Dex Web **Connections**, create the `gitcode` connection. Sign in at [GitCode](https://gitcode.com), click your avatar, then open **个人设置 > 访问令牌 > 新建访问令牌**. Choose the minimum project read permission and an expiry, then copy the token into the secret field. Leave the API base URL, timeout, and response size blank for their displayed defaults.

For local testing, build the connector release artifact and pass it to `dexcli dev --connector-release-override gitcode=<artifact-directory>`. The `sampleRepository` mock returns a synthetic public repository.

In Dex Web **Start Flow**, choose `GitCodeRepository`, supply a unique Flow ID, and enter:

```json
{"owner":"example","repo":"sample"}
```

Generate the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/repository/flow/workflow.go --schema-version 2.0 --json --out /tmp/gitcode-repository-fdg
```
