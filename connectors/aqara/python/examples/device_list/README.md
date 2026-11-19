# Aqara device list Flow

Configure the static `aqara` connection in Dex Web Connections with the four credentials from the [Aqara developer platform](https://opendoc.aqara.com/en/docs/developmanual/apiIntroduction/APIUsageGuide.html). Run `python -m examples.device_list.worker` from this package's Python project after starting `dexcli dev`, then start `AqaraDeviceList` with `{"pageNum":2,"pageSize":25}`. The Flow persists the validated device page as an Attribute. The [Flow source](flow.py) is the executable API example.

The integration test uses inert credentials and local provider transport; real Aqara authorization remains unverified.
