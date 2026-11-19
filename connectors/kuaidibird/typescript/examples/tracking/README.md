# KuaidiBird tracking example

This Flow reads tracking events for one carrier waybill and persists the provider's returned state. Its static connection is `kuaidibird`.

Start `dexcli dev -open=false`, configure the `kuaidibird` connection in Dex Web Connections, and run `npm run example:worker` and `npm run example:start` in separate terminals. Replace the sample carrier code and waybill in `start.ts` with your own values for a real provider query.
