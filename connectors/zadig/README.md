# Zadig connector

[Zadig](https://koderover.com/terms) is operated by KodeRover (Shanghai) Information Technology Co., Ltd. This Go connector calls the official [Zadig v4.3 project-list API](https://docs.koderover.com/zadig/cn/Zadig%20v4.3/api/project/) to read one page of project keys, names, and deployment types. It does not start workflows or expose project configuration and secrets.

## Connection

Use an organization-operated Zadig deployment reachable through HTTPS. Its address is the Dex `baseUrl`; HTTP is accepted only for loopback tests. The [official API guide](https://docs.koderover.com/zadig/cn/Zadig%20v4.3/api/usage/) says to open the top-right user menu, choose **账号设置**, and copy the API Token. Enter it as the secret Dex `api_key` credential. Zadig's guide does not describe token scopes; use a user account whose project permissions are limited to read-only access. Leave timeout and response limit blank for displayed defaults. Rotate a compromised token in account settings.

## Go

The [project catalog Flow](go/examples/project-catalog/) is a runnable Worker with static Dex Web connection name `zadig`. Input `pageNum` and `pageSize` default to 1 and 20; `listed` completes the Flow. Optional branches fail unless an application wires a business response. Transient failures retry safely. The local mock integration uses synthetic project data and a real Dex Worker and Client. Live deployment authorization and project values remain unverified without a user-controlled connection.
