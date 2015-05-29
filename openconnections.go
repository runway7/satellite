package main

import (
	"fmt"
	"net/http"

	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
)

func NewOpenConnectionsHandler(pool *redis.Pool) func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			c := pool.Get()
			defer c.Close()
			connectionCount, _ := redis.String(c.Do("GET", "connection-count"))

			fmt.Fprintf(w, connectionCount)
		}
	}
}
