# Wapi TypeScript example

This runnable Flow calls `listVehicleBrands` with static connection name `wapi` and persists the provider result in a Dex Attribute. Configure the connection through Dex Web, start `dexcli dev -open=false`, then run `npm run example:worker` and `npm run example:start -- '{}'` in separate terminals. Credentials remain outside Flow input and state. Optional provider rejection branches are unwired.
