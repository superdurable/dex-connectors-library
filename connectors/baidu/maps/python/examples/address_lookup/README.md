# Baidu Maps address lookup Flow

Configure the static `baidu-maps` connection in Dex Web Connections with a server-side AK enabled for Geocoding API v3. Run `python -m examples.address_lookup.worker` from this package's Python project after starting `dexcli dev`, then start `BaiduMapsAddressLookup` with `{"address":"北京市海淀区上地十街10号","city":"北京市"}`. The Flow persists the validated BD-09 coordinate as an Attribute. The [Flow source](flow.py) is the executable API example.

The integration test uses an inert AK and local provider transport; real provider authorization remains unverified.
