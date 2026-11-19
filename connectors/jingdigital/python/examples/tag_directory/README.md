# JINGdigital tag directory Flow

`worker.py` loads the static `jingdigital` connection. `TagDirectoryFlow` calls `new_list_tags_step` and persists the page in a Dex Attribute. `integration_test.py` verifies an authorization 503 retry followed by token exchange and persisted tag output against `dexcli dev -open=false` with a mock provider response.
