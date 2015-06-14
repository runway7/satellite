package main

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
	"github.com/runway7/satellite/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	redisUrlKey := os.Getenv("SATELLITE_REDIS_URL_KEY")
	redisURL := os.Getenv(redisUrlKey)

	if redisURL == "" {
		redisURL = "localhost:6379"
	}
	u, err := url.Parse(redisURL)
	if err != nil {
		panic(err)
	}
	redisHost := u.Host
	redisPassword, _ := u.User.Password()
	pool := newPool(redisHost, redisPassword)

	token := os.Getenv("TOKEN")

	broadcaster := NewSatelliteHandler(pool, token)
	stats := NewStatsHandler(pool)

	router := httprouter.New()
	router.HandlerFunc("GET", "/", stats)
	router.HandlerFunc("GET", "/:channel", broadcaster)
	router.HandlerFunc("POST", "/:channel", broadcaster)

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "3001"
	}
	http.ListenAndServe(":"+port, router)
}

func newPool(host, password string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     2,
		MaxActive:   5,
		IdleTimeout: 5 * time.Second,
		Wait:        true,
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", host)
			if err != nil {
				log.Println(err)
				return nil, err
			}
			if password != "" {
				if _, err := c.Do("AUTH", password); err != nil {
					log.Println(err)
					c.Close()
					return nil, err
				}
			}
			return c, err
		},
	}
}
