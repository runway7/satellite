# Satellite
Go app to broadcast messages to all connected web clients using SSE and Redis PubSub

## Installation
[![Deploy](https://www.herokucdn.com/deploy/button.png)](https://heroku.com/deploy)

Or:

1. Install Redis
2. `go get github.com/runway7/satellite`
3. ????

## Usage
1. Save authentication token as `$TOKEN` (you'll use this when you broadcast a message)
2. Redis url key as `$REDIS_URL_KEY` (for example, `"REDIS_URL"`)
3. Redis url as `$REDIS_URL` (if your Redis url key was `"REDIS_URL"`)

### Create a channel / Broadcast a message
Create a `POST` request to `/broadcast/channelName` with `token` and `message` as form values

### Subscribe to a channel / Listen for messages
Create a `GET` request to `/broadcast/channelName`

## How it works
1. A goroutine listens for Redis pubsub messages
   1. It passes it to the appropriate channel 
   2. goroutines that are listening on that channel pass it to their client via SSE

2. When a client connects using a GET request, a goroutine waits for messages on a channel (which it creates if necessary)

3. When a POST request is received with token and message, the goroutine publishes it using Redis pubsub

The app uses [Strobe](github.com/sudhirj/strobe) for the channels, [Redigo](github.com/garyburd/redigo/redis) to pass messages between servers, and [SSE](github.com/manucorporat/sse) to broadcast the messages to the web user.
