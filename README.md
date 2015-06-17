# Satellite
Go app to broadcast messages to all connected web clients using SSE and Redis PubSub

## Installation
[![Deploy](https://www.herokucdn.com/deploy/button.png)](https://heroku.com/deploy)

Or:

1. Install Redis - [http://redis.io/download](http://redis.io/download)
2. `go get github.com/runway7/satellite`

## Configuration
1. `TOKEN`: An authentication token that you'll use you broadcast a message
2. `REDIS_URL`: The URL of your Redis installation
3. `REDIS_URL_KEY`: If you use a hosting provider that sets your Redis URL environment variable under another name, like `REDIGO_URL`, set this to the name of the variable.

### Subscribe to a channel / Listen for messages
Create a `GET` request to `/channel`

### Broadcast a message
Create a `POST` request to `/channel` with `token` and `message` as parameters values. Channels that do not exist are created on the fly.

Satellite app uses [Strobe](https://github.com/sudhirj/strobe) for channel fan-out, [Redigo](https://github.com/garyburd/redigo) to pass messages between servers, and [SSE](https://github.com/manucorporat/sse) for SSE formatting.
