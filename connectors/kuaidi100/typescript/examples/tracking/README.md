# Kuaidi100 tracking example

This Flow reads one parcel's tracking state and events. Its static connection is `kuaidi100`.

Start `dexcli dev -open=false`, configure the `kuaidi100` connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. Replace the sample carrier code, waybill, and phone in `start.ts` with your own values for a real query. Respect the provider's 30-minute interval between queries for one parcel.
