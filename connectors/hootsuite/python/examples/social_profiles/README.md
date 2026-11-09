# Hootsuite social profiles Flow (Python)

This runnable Flow reads social profiles through the static `hootsuite` connection and stores the typed result in a registered Attribute. Configure OAuth in Dex Web using the [connector guide](../../../README.md).

From this Python package, run `dexcli visualize examples/social_profiles/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/hootsuite-python-fdg --open=false --python .venv/bin/python`, then `uv run python -m examples.social_profiles.worker`. Start `HootsuiteSocialProfilesPython` with `{}` and a unique Flow ID. The real Dex test with a fake provider is `uv run pytest examples/social_profiles/integration_test.py`. Live Hootsuite authorization remains unverified.
