package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/heroku/slog"

	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/manucorporat/sse"
	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/sudhirj/strobe"
)

type satellite struct {
	channels  map[string]*strobe.Strobe
	redisPool *redis.Pool
	token     string
	sync.Mutex
}

func (s *satellite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	channelName := r.URL.Path

	channel, ok := s.channels[channelName]
	if !ok {
		s.Lock()
		channel = strobe.NewStrobe()
		s.channels[channelName] = channel
		s.Unlock()
	}
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
		defer channel.Off(listener)
		defer s.recordEvent("finish", channelName)
		s.recordEvent("subscribe", channelName)

		sse.Encode(w, sse.Event{
			Event: "open",
			Data:  "START",
		})
		f.Flush()
		killSwitch := time.After(5 * time.Minute)
		for {
			select {
			case <-listener:
				//case msg := <-listener:
				sse.Encode(w, sse.Event{
					Event: "message",
					Data:  "PONG",
					//msg,
				})
				f.Flush()
				go s.recordEvent("send", channelName)
			case <-closer.CloseNotify():
				go s.recordEvent("close", channelName)
				return
			case <-time.After(10 * time.Second):
				sse.Encode(w, sse.Event{
					Event: "message",
					Data:  "PING",
				})
				f.Flush()
				go s.recordEvent("ping", channelName)
			case <-killSwitch:
				go s.recordEvent("kill", channelName)
				return
			}
		}
	case "POST":
		if r.FormValue("token") == s.token {
			conn := s.redisPool.Get()
			conn.Do("PUBLISH", channelName, r.FormValue("message"))
			conn.Close()
			go s.recordEvent("publish", channelName)
		} else {
			http.Error(w, "Authentication Error", http.StatusUnauthorized)
			return
		}
	}
}

func (b *satellite) recordEvent(event string, channelName string) {
	// the layout string shows by example how the reference time should be represented
	// where reference time is Mon Jan 2 15:04:05 -0700 MST 2006
	// http://golang.org/pkg/time/#example_Time_Format

	c := b.redisPool.Get()
	defer c.Close()
	c.Send("INCR", event+"-"+time.Now().UTC().Format("200601021504"))
	c.Send("INCR", event+"-"+time.Now().UTC().Format("2006010215"))
	c.Send("INCR", event+"-"+time.Now().UTC().Format("20060102"))
	c.Send("INCR", event+"-"+time.Now().UTC().Format("200601"))
	c.Send("INCR", event+"-"+time.Now().UTC().Format("2006"))
	c.Flush()
}

func (b *satellite) logListeners() {
	for {
		select {
		case <-time.After(5 * time.Second):
			ctx := slog.Context{}
			listenerCount := 0
			for _, strobe := range b.channels {
				listenerCount += strobe.Count()
			}
			ctx.Count("listeners", listenerCount)
			log.Println(ctx)
		}
	}
}

func (b *satellite) log() {
	for {
		select {
		case <-time.After(60 * time.Second):
			t := time.Now().UTC().Add(-time.Minute).Format("200601021504")
			c := b.redisPool.Get()
			ctx := slog.Context{}
			pubCount, _ := redis.Int(c.Do("GET", "publish-"+t))
			ctx.Count("publishes.minute", pubCount)
			sendCount, _ := redis.Int(c.Do("GET", "send-"+t))
			ctx.Count("sends.minute", sendCount)
			pingCount, _ := redis.Int(c.Do("GET", "ping-"+t))
			ctx.Count("pings.minute", pingCount)
			closeCount, _ := redis.Int(c.Do("GET", "close-"+t))
			ctx.Count("closes.minute", closeCount)
			log.Println(ctx)
			c.Close()
		}
	}
}

func (b *satellite) start() {
	conn := b.redisPool.Get()
	defer conn.Close()
	psc := redis.PubSubConn{Conn: conn}
	psc.PSubscribe("*")
	go b.log()
	go b.logListeners()
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
	broker := &satellite{channels: make(map[string]*strobe.Strobe), redisPool: pool, token: token}
	go broker.start()
	return broker.ServeHTTP
}
