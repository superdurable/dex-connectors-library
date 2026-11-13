# NetEase Yunxin user profile Flow

This runnable Flow uses the static `netease` connection to read one IM account profile. The `fetched` branch stores the account ID and nickname in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Run from this module:

```sh
go run ./examples/user-profile
```

In Dex Web **Connections**, create the `netease` connection. Sign in at the [Yunxin console](https://app.yunxin.163.com), select an application with IM enabled, and open **App Key 管理**. Enter the App Key and App Secret in their secret fields. Leave the API base URL, timeout, and response size blank for their displayed defaults. Use the documented overseas service domain only if the application's IM data is overseas.

For local testing, build the connector release artifact and pass it to `dexcli dev --connector-release-override netease=<artifact-directory>`. The mock `sampleUser` returns synthetic data.

In Dex Web **Start Flow**, choose `NetEaseYunxinUserProfile`, supply a unique Flow ID, and enter:

```json
{"accountId":"sample-user"}
```

Generate the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/user-profile/flow/workflow.go --schema-version 2.0 --json --out /tmp/netease-user-profile-fdg
```
