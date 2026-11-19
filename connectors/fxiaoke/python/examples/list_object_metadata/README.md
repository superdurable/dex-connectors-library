# FXiaoke CRM object metadata Flow

`worker.py` loads the static `fxiaoke` project connection from Dex configuration and runs `ObjectMetadataFlow`. The Flow uses `new_list_object_metadata_step` and stores CRM object names and availability in a Dex Attribute. Generate the Flow Definition Graph with `dexcli visualize --open=false` from this example's Worker entrypoint. Run `integration_test.py` against `dexcli dev -open=false` to verify 503 retry and persisted completion with a mock FXiaoke HTTP service.
