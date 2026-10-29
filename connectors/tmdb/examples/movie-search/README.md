# TMDB movie-search example

This runnable Worker starts `TMDBMovieSearch` with typed `moviesearch.Request`, calls
`tmdb.NewSearchMoviesStep`, and completes with `tmdb.MoviePage`. The
static Dex Web connection name is `tmdb`. Optional failure branches are
unwired and fail the Flow when selected.

## Generate the strict FDG 2.0 definition

From `connectors/tmdb` with a current Dex CLI:

```bash
mkdir -p build
dexcli visualize ./examples/movie-search/flow/workflow.go --schema-version 2.0 --json --out ./build/movie-search
```

Check that the command reports `valid: true`. A local source may warn that a
connector release is required until this module is published.

## Open the unreleased connector in Dex Web

Until this connector has a published release, generate local release metadata
and pass it to the Dex CLI. From the repository root:

```bash
mkdir -p /tmp/tmdb-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/tmdb/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/tmdb \
  --version v0.1.0 --tag connectors/tmdb/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/tmdb-release/connector-release.json \
  --digest-output /tmp/tmdb-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/tmdb/build" \
  --connector-release-override tmdb=/tmp/tmdb-release
```

## Configure and run

Create the `tmdb` connection in Dex Web. Start at [https://www.themoviedb.org/settings/api](https://www.themoviedb.org/settings/api),
follow **Account Settings > API > API Read Access Token**, and paste the secret token in `api_key`.
Leave the displayed configuration defaults blank. The token is never a Flow input.

From this module, with the Dex project environment configured, run:

```bash
go run ./examples/movie-search
```

Start `TMDBMovieSearch` in Dex Web with a JSON input matching `tmdb.SearchMoviesInput`.
The mock backed real Dex test is `flow/workflow_mock_integration_test.go` and
CI runs it with the integration tag.
