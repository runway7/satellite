package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
	"log"

	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/manucorporat/sse"
	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/sudhirj/strobe"
)

type broker struct {
	channels  map[string]*strobe.Strobe
	redisPool *redis.Pool
	token     string
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
		defer b.recordEvent("finish",channelName)
		b.recordEvent("subscribe",channelName)
		log.Println("Subscription started")
		for {
			select {
			case msg := <-listener:
				log.Println("Sending a message ("+channelName+")")
				sse.Encode(w, sse.Event{
					Event: "message",
					Data:  msg,
				})
				f.Flush()
				go b.recordEvent("send",channelName)
			case <-closer.CloseNotify():
				log.Println("Closing "+channelName)
				go b.recordEvent("close", channelName)
				return
			case <-time.After(300 * time.Second):
				go b.recordEvent("kill",channelName)
				return
			}
		}
	case "POST":
		if r.FormValue("token") == b.token {
			conn := b.redisPool.Get()
			defer conn.Close()
			conn.Do("PUBLISH", channelName, r.FormValue("message"))
			go b.recordEvent("publish", channelName)
		} else {
			http.Error(w, "Authentication Error", http.StatusUnauthorized)
			return
		}
	}
}

func (b *broker) recordEvent(event string, channelName string) {
	// layout shows by example how the reference time should be represented
	// where reference time is Mon Jan 2 15:04:05 -0700 MST 2006
	// http://golang.org/pkg/time/#example_Time_Format
	layouts := []string{"200601021504", "2006010215", "20060102", "200601", "2006"}
	for _, layout := range layouts {
		go func (e,l string) {
			conn := b.redisPool.Get()
			defer conn.Close()
			conn.Do("INCR", e+"-"+time.Now().UTC().Format(l))
		} (event, layout)
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
