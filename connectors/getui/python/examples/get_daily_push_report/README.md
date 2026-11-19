# Getui daily push report Flow

`worker.py` loads the static `getui` project connection from Dex configuration and runs `DailyPushReportFlow`. The Flow uses `new_get_daily_push_report_step` and stores prior-day aggregate totals in a Dex Attribute. Generate the Flow Definition Graph with `dexcli visualize --open=false` from this example's Worker entrypoint. Run `integration_test.py` against `dexcli dev -open=false` to verify 503 retry and persisted completion with a mock Getui HTTP service.
