package main

import (
	"fmt"
	"net/http"

	"github.com/garyburd/redigo/redis"
)

func NewStatsHandler(pool *redis.Pool) func(w http.ResponseWriter, req *http.Request) {
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
