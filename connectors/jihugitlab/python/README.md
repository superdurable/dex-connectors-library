# JiHu GitLab Python connector

This package provides the read-only `listMemberProjects` Query for JiHu GitLab. Configure a project connection with a personal access token and an HTTPS origin according to the [company guide](../README.md). The operation-specific `new_list_member_projects_step` returns stable project IDs, names, paths, and a next-page number. The runnable example wires the happy `listed` branch; `providerRejected`, `invalidResponse`, and `defect` are optional.

Run `uv sync --locked` and `uv run pytest` in this directory. The [member-project Flow](examples/member_projects/) includes a Worker and a real Dex retry/persistence integration test with a mock JiHu GitLab response. Live provider authorization has not been verified.
