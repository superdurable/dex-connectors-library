# IsTero account status Flow

`worker.py` loads the static `istero` project connection. `AccountStatusFlow` calls `new_get_account_status_step` and persists the account usage output in a Dex Attribute. `integration_test.py` proves 503 retry and persisted completion using `dexcli dev -open=false` and a mock ISAS response.
