package main

import (
	"net/http"
	"os"
	"strings"
	"time"
	"log"

	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
)

func newPool(server string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     300,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			log.Println("Dialling redis - "+server)
			c, err := redis.Dial("redis", server)
			if err != nil {
				log.Println(err)
				return nil, err
			}
			return c, err
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}

func main() {
	router := httprouter.New()

	redisUrlKey := os.Getenv("REDIS_URL_KEY")
	redisURL := os.Getenv(redisUrlKey)
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	pool := newPool(redisURL)

	token := os.Getenv("TOKEN")
	broadcaster := NewBroadcastHandler(pool, token)
	router.HandlerFunc("GET", "/broadcast/:channel", broadcaster)
	router.HandlerFunc("POST", "/broadcast/:channel", broadcaster)

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "3001"
	}
	http.ListenAndServe(":"+port, router)
}
