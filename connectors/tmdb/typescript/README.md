# TMDB TypeScript connector

`@superdurable/connector-tmdb` provides `searchMovies` and `getMovie` Queries for Dex. The package version is `v0.1.0`; the Go connector remains at its own version. Install with `npm install @superdurable/connector-tmdb`.

Use `newProjectConnection(project, 'tmdb')` to bind the Dex Web connection; its `api_key` secret is resolved for each call. The project configuration honors `baseUrl`, `timeout` (Go duration), and `maxResponseBytes`. The package exposes generated `newSearchMoviesStep` and `newGetMovieStep` factories, typed models, definitions, and mock cases.

The runnable [movie search example](examples/movie-search/README.md) shows the Worker, static connection, Flow, and real Dex integration test. Optional `providerRejected`, `invalidResponse`, `defect`, and `notFound` branches are unwired by default and fail the Flow when selected. HTTP 408, 429, 5xx, and transport failures retry; malformed or oversized 200 responses select `invalidResponse`.

The conformance suite replays Go-recorded provider requests and outcomes. Live TMDB authorization and API behavior remain unverified; use a user-controlled Dex Web connection to verify them.
