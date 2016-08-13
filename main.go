package main

import (
	"net/http"
	"os"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sns"
	"github.com/aws/aws-sdk-go/service/sqs"

	"github.com/garyburd/redigo/redis"
	"github.com/rs/cors"
)

func main() {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	session := session.New(&aws.Config{
		Region: aws.String(os.Getenv("AWS_REGION")),
		Credentials: credentials.NewStaticCredentials(
			os.Getenv("AWS_ACCESS_KEY_ID"),
			os.Getenv("AWS_SECRET_ACCESS_KEY"),
			"",
		),
	})
	outbox := os.Getenv("OUTBOX_ARN")
	inbox := os.Getenv("INBOX_QUEUE_URL")
	configBucket := "satellite-config-" + os.Getenv("AWS_REGION")
	redisPool := newPool(redisURL)
	sqsClient := sqs.New(session)
	snsClient := sns.New(session)
	s3Client := s3.New(session)

	satellite := NewSatellite(outbox, configBucket)
	satellite.SetRedisPool(redisPool)
	satellite.SetSQSClient(sqsClient)
	satellite.SetSNSClient(snsClient)
	satellite.SetS3Client(s3Client)

	go satellite.StartRedisListener()
	go satellite.StartSQSListener(inbox)

	port := os.Getenv("PORT")
	if port == "" {
		port = "7288"
	}
	handler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET"},
		AllowCredentials: true,
	}).Handler(satellite)

	log.WithFields(log.Fields{
		"event":  "sat.start",
		"region": os.Getenv("AWS_REGION"),
	}).Info()

	log.Fatal(http.ListenAndServe(":"+port, handler))
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
