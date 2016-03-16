package main

import (
	"net/http"
	"sync"
	"time"

	"log"

	"github.com/garyburd/redigo/redis"
	"github.com/manucorporat/sse"
	"github.com/sudhirj/strobe"
)

type satellite struct {
	channels  map[string]*strobe.Strobe
	redisPool *redis.Pool
	token     string
	sync.RWMutex
}

func (s *satellite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	channelName := r.URL.Path
	s.Lock()
	channel, ok := s.channels[channelName]
	if !ok {
		channel = strobe.NewStrobe()
		s.channels[channelName] = channel
	}
	s.Unlock()
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

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		listener := channel.Listen()
		defer listener.Close()

		sse.Encode(w, sse.Event{
			Event: "open",
			Data:  "START",
		})
		f.Flush()
		killSwitch := time.After(5 * time.Minute)
		for {
			select {
			case m := <-listener.Receiver():
				sse.Encode(w, sse.Event{
					Event: "message",
					Data:  m,
				})
				f.Flush()
			case <-closer.CloseNotify():
				return
			case <-time.After(10 * time.Second):
				sse.Encode(w, sse.Event{
					Event: "message",
					Data:  "PING",
				})
				f.Flush()
			case <-killSwitch:
				return
			}
		}
	case "POST":
		if r.FormValue("token") == s.token {
			conn := s.redisPool.Get()
			conn.Do("PUBLISH", channelName, r.FormValue("message"))
			conn.Close()
		} else {
			http.Error(w, "Authentication Error", http.StatusUnauthorized)
			return
		}
	}
}

func (s *satellite) start() {
	r := s.redisPool.Get()
	defer r.Close()
	psc := redis.PubSubConn{Conn: r}
	psc.PSubscribe("*")
	defer psc.Close()
	for {
		switch n := psc.Receive().(type) {
		case redis.PMessage:
			s.RLock()
			channel, ok := s.channels[n.Channel]
			s.RUnlock()
			if ok {
				channel.Pulse(string(n.Data))
			}
		case error:
			log.Printf("error: %v\n", n)
			return
		}
	}
}

// NewSatelliteHandler creates a new handler that handles pub sub
func NewSatelliteHandler(pool *redis.Pool, token string) func(w http.ResponseWriter, req *http.Request) {
	s := &satellite{channels: make(map[string]*strobe.Strobe), redisPool: pool, token: token}
	go s.start()
	return s.ServeHTTP
}
