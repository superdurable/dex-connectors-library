# LinkedIn Python connector

The `getAuthenticatedProfile` Query reads bounded OpenID Connect UserInfo claims and requires a verified email. Install `dex-connector-linkedin==0.1.0`. Project connections use generated OAuth refresh and credential wiring from Connector SDK 0.5.0. The runnable example is in `examples/authenticated_profile`.

Run `uv sync --locked` and `uv run pytest -q tests` from this directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest -q examples/authenticated_profile/integration_test.py`. The example uses the generated OAuth project connection and a static connection name. Live LinkedIn authorization and UserInfo behavior have not been verified.
