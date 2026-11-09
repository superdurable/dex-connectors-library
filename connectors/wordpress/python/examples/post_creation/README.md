# Post creation Flow

`PostCreationFlow` uses the static Dex Web `wordpress` connection and `new_create_post_step`. Register it with `examples.worker`, then start a Flow with `CreatePostInput`. The result is persisted as `CreatedPost`.

Generate strict FDG 2.0: `dexcli visualize examples/post_creation/flow.py --schema-version 2.0 --json --out /tmp/wordpress-python-post_creation-fdg --lint app`.
