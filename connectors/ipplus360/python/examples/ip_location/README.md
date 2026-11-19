# IPPlus360 location Flow

`worker.py` loads the static `ipplus360` project connection from Dex configuration and runs `IPLocationFlow`. The Flow uses `new_lookup_ip_location_step` and stores the coarse location output in a Dex Attribute. Generate the Flow Definition Graph with `dexcli visualize --open=false` from this example's Worker entrypoint. Run `integration_test.py` against `dexcli dev -open=false` to verify 503 retry and persisted completion with a mock IPPlus360 HTTP service.
