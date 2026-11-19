# Fanyigou balance Flow

`worker.py` loads the static `fanyigou` project connection from Dex configuration and runs `AccountBalanceFlow`. The Flow uses `new_get_account_balance_step` and stores the resource balance in a Dex Attribute. Generate the Flow Definition Graph with `dexcli visualize --open=false` from this example's Worker entrypoint. Run `integration_test.py` against `dexcli dev -open=false` to verify 503 retry and persisted completion with a mock Fanyigou HTTP service.
