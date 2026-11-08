# Azure DevOps Python connector

This package implements `listProjects` and `getWorkItem` for Dex Python Flows. It replays the Go-recorded requests, branches, retry decisions, and typed outputs. Project results contain only ID, name, state, page size, and offset; work item results contain only ID, title, state, and type.

Sign in to [Azure DevOps](https://dev.azure.com/), open your organization, and copy its URL into Dex `baseUrl`. Under user settings > Personal access tokens > New Token, create an organization-scoped PAT with Project and Team (Read) and Work Items (Read) scopes. Save the one-time token in Dex secret `personal_access_token`. See the [connector README](../README.md) and [runnable example](examples/project_discovery/README.md). Live provider authorization remains unverified.
