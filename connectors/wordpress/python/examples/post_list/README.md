# Post list Flow

`PostListFlow` uses the static Dex Web `wordpress` connection and `new_list_posts_step`. Register it with `examples.worker`, then start a Flow with `ListPostsInput`. The result is persisted as `PostPage`.

Generate strict FDG 2.0: `dexcli visualize examples/post_list/flow.py --schema-version 2.0 --json --out /tmp/wordpress-python-post_list-fdg --lint app`.
