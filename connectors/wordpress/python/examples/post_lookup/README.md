# Post lookup Flow

`PostLookupFlow` uses the static Dex Web `wordpress` connection and `new_get_post_step`. Register it with `examples.worker`, then start a Flow with `GetPostInput`. The result is persisted as `Post`.

Generate strict FDG 2.0: `dexcli visualize examples/post_lookup/flow.py --schema-version 2.0 --json --out /tmp/wordpress-python-post_lookup-fdg --lint app`.
