package main

import (
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
)

func newPool(server string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     2,
		MaxActive:   8,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			log.Println("Dialling redis - " + server)
			c, err := redis.Dial("tcp", server)
			if err != nil {
				log.Println(err)
				return nil, err
			}
			if password := os.Getenv("REDIS_PASSWORD"); password != "" {
				if _, err := c.Do("AUTH", password); err != nil {
					log.Println(err)
					c.Close()
					return nil, err
				}
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
	runtime.GOMAXPROCS(runtime.NumCPU())
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

	openConnections := NewOpenConnectionsHandler(pool)
	router.HandlerFunct("GET", "/openconnections", openConnections)

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "3001"
	}
	http.ListenAndServe(":"+port, router)
}
