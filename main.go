package main

import (
	"net/http"
	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/julienschmidt/httprouter"
	"github.com/urfave/negroni"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sns"
	"github.com/aws/aws-sdk-go/service/sqs"

	"github.com/rs/cors"
)

func main() {

	awsSession, err := session.NewSession()
	if err != nil {
		log.Fatal(err)
	}

	authorizer := Authorizer{
		configBucket: os.Getenv("CONFIG_BUCKET"),
		s3Client:     s3.New(awsSession),
	}

	satellite := NewSatellite(SatelliteConfig{
		sqsClient:   sqs.New(awsSession),
		snsClient:   sns.New(awsSession),
		s3Client:    s3.New(awsSession),
		s3Uploader:  s3manager.NewUploader(awsSession),
		eventBucket: os.Getenv("EVENT_BUCKET"),
		authorizer:  authorizer,
	})

	//go satellite.StartSQSListener(inbox)
	router := httprouter.New()
	router.GET("/", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {

	})
	router.GET("/:realm/:topic", satellite.Listen)
	router.POST("/:realm/:topic", satellite.Post)

	port := os.Getenv("PORT")
	if port == "" {
		port = "7288"
	}
	corsMiddleware := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowCredentials: true,
	})
	n := negroni.Classic()
	n.UseHandler(router)
	n.Use(corsMiddleware)
	log.Fatal(http.ListenAndServe(":"+port, n))
}
