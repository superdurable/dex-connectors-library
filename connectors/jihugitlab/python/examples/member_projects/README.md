# JiHu GitLab member projects Flow

`worker.py` loads the static `jihugitlab` connection. `MemberProjectsFlow` calls `new_list_member_projects_step` and persists the page in a Dex Attribute. `integration_test.py` verifies 503 retry and persisted completion against `dexcli dev -open=false` with a mock provider response.
