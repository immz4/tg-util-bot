# Telegram utility bot

Bot for forwarding messages between chats and channels, boring and simple

## Configuration

Example configuration, which should be provided in APP_CONFIG env var

```toml
token = ""
port = "3000"

[resend]
enabled = true
keywords = ["quote", "q"]
from = [ -4000000000 ]
to = [ -4000000001 ]

[resend.command]
text = "/1"
description = "Save this message"

[feedback]
enabled = true
to = [ -4000000001 ]
```

## Limitations and TODOs

- Doesn't account for message grouping
- No live update for settings
- No user permissions for commands