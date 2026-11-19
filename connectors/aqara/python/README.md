# Aqara Python connector

`dex-connector-aqara` exposes `listDevices` as a Dex Query Step. The [official Device Management API](https://opendoc.aqara.com/en/docs/developmanual/apiDocument/DeviceManagement.html) documents `query.device.info`; the [API usage guide](https://opendoc.aqara.com/en/docs/developmanual/apiIntroduction/APIUsageGuide.html) documents the mainland China endpoint and signed headers. Configure private credentials in Dex Web Connections; none enters Flow state or logs.

See the [runnable Flow](examples/device_list/) for a Worker and static connection name. Real provider authorization has not been verified locally.

## Strict FDG 2.0 generation

From this package directory, run:

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/aqara-python-fdg --python .venv/bin/python examples/device_list/flow.py
```
