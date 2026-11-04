# TMDB movie search, TypeScript

`MovieSearchFlow` accepts `{ "query": "Jack Reacher", "page": 1 }`, searches TMDB, and returns a movie page. Its static Dex Web connection name is `tmdb`. Configure that connection with the API Read Access Token from [TMDB account settings](https://www.themoviedb.org/settings/api); the secret is resolved at call time and never enters Flow input or output.

Run `npm ci`, start `dexcli dev`, then run `npm run example:movie-search` from this package. Start Flow type `TMDBTypeScriptMovieSearch` with the JSON input above. For a credential-free real Dex Worker and Client test against a local provider stand-in, set `DEX_FLOW_SERVICE_ADDRESS` to the dev server address and run `npm run integration:movie-search`.

Attempt strict FDG 2.0 generation from this package with `dexcli visualize examples/movie-search/flow.ts -language typescript -schema-version 2.0 -lint app -json -out /tmp/tmdb-typescript-movie-search`. The current CLI reports `dynamic_step_target` and `unknown_step_target` for the registered SDK QueryStep, with a related `dynamic_class` diagnostic. The real Dex Worker and Client integration test verifies the Flow path. The provider Step declares one group and explanation; optional failure branches are unwired so they fail the Flow.
