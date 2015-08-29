package main

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/julienschmidt/httprouter"
	"github.com/rs/cors"
)

func main() {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURLKey := os.Getenv("SATELLITE_REDIS_URL_KEY")
		redisURL = os.Getenv(redisURLKey)
	}
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

	router := httprouter.New()
	router.HandlerFunc("GET", "/:channel", broadcaster)
	router.HandlerFunc("POST", "/:channel", broadcaster)

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "3001"
	}

	handler := cors.Default().Handler(router)
	err = http.ListenAndServe(":"+port, handler)
	if err != nil {
		log.Fatal(err)
	}
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
