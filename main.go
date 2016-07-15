package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"

	"github.com/garyburd/redigo/redis"
	"github.com/rs/cors"
)

func main() {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	redisPool := newPool(redisURL)
	sqsClient := newSQSClient()

	queueURL := os.Getenv("SQS_QUEUE_URL")

	satellite := NewSatellite()
	go satellite.StartRedisListener(redisPool)
	go satellite.StartSQSListener(sqsClient, queueURL)

	port := os.Getenv("PORT")
	if port == "" {
		port = "7288"
	}
	handler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET"},
		AllowCredentials: true,
	}).Handler(satellite)

	log.Fatal(http.ListenAndServe(":"+port, handler))
}

func newSQSClient() *sqs.SQS {
	return sqs.New(session.New(&aws.Config{
		Region: aws.String(os.Getenv("AWS_REGION")),
		Credentials: credentials.NewStaticCredentials(
			os.Getenv("AWS_ACCESS_KEY_ID"),
			os.Getenv("AWS_SECRET_ACCESS_KEY"),
			"",
		),
	}))
}

func newPool(host string) *redis.Pool {
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
			return c, err
		},
	}
}
