package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/manucorporat/sse"
	"github.com/sudhirj/strobe"
)

type broker struct {
	channels  map[string]*strobe.Strobe
	redisPool *redis.Pool
	token string
	sync.RWMutex
}

func (b *broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	channelName := r.URL.Path
	b.Lock()
	channel, ok := b.channels[channelName]
	if !ok {
		channel = strobe.NewStrobe()
		b.channels[channelName] = channel
	}
	b.Unlock()
	switch r.Method {
	case "GET":
		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		closer, ok := w.(http.CloseNotifier)
		if !ok {
			http.Error(w, "Closing unsupported!", http.StatusInternalServerError)
			return
		}

		listener := channel.Listen()
		defer channel.Off(listener)
		for {
			select {
			case msg := <-listener:
				sse.Encode(w, sse.Event{
					Event: "message",
					Data:  msg,
				})
				f.Flush()
			case <-closer.CloseNotify():
				return
			case <-time.After(300 * time.Second):
				return
			}
		}
	case "POST":
		if (r.FormValue("token") == b.token) {
			conn := b.redisPool.Get()
			defer conn.Close()
			conn.Do("PUBLISH", channelName, r.FormValue("message"))
		} else {
			http.Error(w, "Authentication Error", http.StatusUnauthorized)
			return
		}	
	}
}

func (b *broker) start() {
	conn := b.redisPool.Get()
	defer conn.Close()
	psc := redis.PubSubConn{conn}
	psc.PSubscribe("*")
	for {
		switch n := psc.Receive().(type) {
		case redis.PMessage:
			channel, ok := b.channels[n.Channel]
			if ok {
				channel.Pulse(string(n.Data))
			}
		case error:
			fmt.Printf("error: %v\n", n)
			return
		}

	}
}

// NewBroadcastHandler creates a new handler that handles pub sub
func NewBroadcastHandler(pool *redis.Pool, token string) func(w http.ResponseWriter, req *http.Request) {
	broker := &broker{channels: make(map[string]*strobe.Strobe), redisPool: pool, token: token}
	go broker.start()
	return broker.ServeHTTP
}
