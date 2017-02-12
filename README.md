# slack-blackhole

Remove messages and files in a certain duration for your Slack team.

## Usage

```
$ go build
$ cat config.json
[
        {
                "channel": "dev_null",
                "message_ttl": 600,
                "file_ttl": 600
        },
        {
                "channel": "dev_null_daily",
                "message_ttl": 86400,
                "file_ttl": 86400
        }
]
$ ./slack-blackhole --slack-api-token xoxp-aaa... --defaut-file-ttl $((86400*30)) --config-file config.json
```

### Other options

```
$ ./slack-blackhole --help
Usage of ./slack-blackhole:
  -config-file string
        Configuration file
  -debug
        Debug on
  -debug-slack
        Debug on for Slack
  -default-file-ttl int
        TTL of files for all channel
  -default-message-ttl int
        TTL of messages for all channel
  -dry-run
        Do not delete messages/files
  -slack-api-interval int
        Interval (sec) for api call (default 3)
  -slack-api-token string
        Slack API token
```

All options can be set as environment variables.  Each environment variable
has `BLACKHOLE_` prefix like `BLACKHOLE_DEBUG` for `--debug`.

## Author

Katsuyuki Tateishi <kt@wheel.jp>

