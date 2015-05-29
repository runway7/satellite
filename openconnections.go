package main

import (
	"fmt"
	"net/http"

	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
)

type broker struct {
	redisPool *redis.Pool
}

func (b *broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		c := b.redisPool.Get()
		defer c.Close()
		connectionCount := c.Do("GET", "connection-count")
		
		fmt.Fprintf(w, connectionCount)
	}
}

func NewOpenConnectionsHandler(pool *redis.Pool) func(w http.ResponseWriter, req *http.Request) {
	broker := &broker{redisPool: pool}
	return broker.ServeHTTP
}
