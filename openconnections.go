package main

import (
	"fmt"
	"net/http"

	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
)

func (p *redis.Pool) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		c := p.Get()
		defer c.Close()
		connectionCount := c.Do("GET", "connection-count")
		
		fmt.Fprintf(w, connectionCount)
	}
}

func NewOpenConnectionsHandler(pool *redis.Pool) func(w http.ResponseWriter, req *http.Request) {
	return pool.ServeHTTP
}
