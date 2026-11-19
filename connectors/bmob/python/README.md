# Bmob Python connector

The `dex-connector-bmob` package exposes `CountClassObjectsInput`, `CountClassObjectsOutput`, `new_count_class_objects_step`, and a project connection for the read-only Bmob class count Query. It sends Application ID and REST API Key in request headers. The output contains only a count.

The [runnable Flow](examples/class_object_count/flow.py) uses the static `bmob` connection and persists the count. Its [integration test](examples/class_object_count/integration_test.py) verifies one 503 retry across a real Dex Worker and Client. The [conformance test](tests/test_conformance.py) replays Go-recorded provider requests and outcomes.

## Run with Dex Web

Create a `bmob` connection in Dex Web. From [Bmob](https://www.bmobapp.com/), open your app's **设置 > 应用密钥** and enter the Application ID and REST API Key. Under **控制台 > 应用设置 > 域名管理 > API域名**, copy the app-specific HTTPS API root URL into `baseUrl` without `/1`. Leave timeout and response size blank for the displayed defaults. Use a class name from the app's **数据** section.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
uv run python -m examples.class_object_count.worker
```

In Dex Web start `BmobClassObjectCount` with a unique Flow ID and input `{"className":"GameScore"}`.

## Strict FDG 2.0 generation

```bash
dexcli visualize ./examples/class_object_count/flow.py --schema-version 2.0 --json --out /tmp/bmob-python-fdg
```

The output must report `valid: true`. Tests do not verify live Bmob credentials, class permissions, or production response shape.
