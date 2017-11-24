package main

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/julienschmidt/httprouter"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sns"
	"github.com/aws/aws-sdk-go/service/sqs"

	"github.com/manucorporat/sse"
)

type SatelliteConfig struct {
	sqsClient   *sqs.SQS
	snsClient   *sns.SNS
	s3Client    *s3.S3
	s3Uploader  *s3manager.Uploader
	eventBucket string
	authorizer  Authorizer
}

// Satellite is a broadcaster that provides a http.Handler
type Satellite struct {
	antennas     map[string]*Antenna
	antennaMutex sync.RWMutex
	config       SatelliteConfig
}

type Config struct {
	Password string `json:"password"`
}

type Authorizer struct {
	s3Client     *s3.S3
	configBucket string
}

func (s *Authorizer) load(satelliteID string) (config Config, err error) {
	configResponse, err := s.s3Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.configBucket),
		Key:    aws.String(satelliteID),
	})
	if err == nil {
		err = json.NewDecoder(configResponse.Body).Decode(&config)
	}
	return
}

func (s *Satellite) Post(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	config, err := s.config.authorizer.load(params.ByName("satellite"))
	if err != nil {
		http.Error(w, "Satellite ID not recognized", http.StatusNotFound)
		return
	}
	if subtle.ConstantTimeCompare([]byte(config.Password), []byte(params.ByName("token"))) == 1 {
		http.Error(w, "Wrong token", http.StatusForbidden)
		return
	}
	eventID := params.ByName("event_id")
	if eventID == "" {
		eventID = strconv.FormatInt(time.Now().UnixNano(), 26)
	}
	s.config.s3Uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(s.config.eventBucket),
		Key: aws.String(strings.Join([]string{
			params.ByName("satellite"),
			params.ByName("topic"),
			eventID,
		}, "/")),
		Body: r.Body,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Satellite) Listen(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	path := strings.Join([]string{params.ByName("satellite"), params.ByName("topic")}, "/")

	s.antennaMutex.RLock()
	antenna, ok := s.antennas[path]
	s.antennaMutex.RUnlock()

	if !ok {
		s.antennaMutex.Lock()
		antenna = NewAntenna()
		s.antennas[path] = antenna
		s.antennaMutex.Unlock()
	}

	_, ok = w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusExpectationFailed)
		return
	}

	closer, ok := w.(http.CloseNotifier)
	if !ok {
		http.Error(w, "Closing unsupported!", http.StatusExpectationFailed)
		return
	}

	killSwitch := time.After(10 * time.Minute)
	beam := antenna.Add(w)
	defer antenna.Remove(beam)
	for {
		select {
		case <-closer.CloseNotify():
			return
		case <-time.After(45 * time.Second):
			beam.Pulse(sse.Event{
				Event: "PING",
			})
		case <-killSwitch:
			return
		}
	}
}

// Publish sends the given message to all subscribers of the given topic
func (s *Satellite) Publish(topic, message string) {
	s.antennaMutex.RLock()
	antenna, ok := s.antennas[topic]
	s.antennaMutex.RUnlock()
	if ok {
		go antenna.Pulse(sse.Event{
			Id:    strconv.Itoa(int(time.Now().UnixNano())),
			Event: "message",
			Data:  message,
		})
	}
}

type snsMessage struct {
	Channel string `json:"Subject"`
	Data    string `json:"Message"`
}

// StartSQSListener listens for SQS messages and publishes them to Redis
func (s *Satellite) StartSQSListener(inbox string) {
	for {
		log.WithField("event", "sat.sqs.inbox.open").Info()
		resp, err := s.config.sqsClient.ReceiveMessage(&sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(inbox),
			WaitTimeSeconds:     aws.Int64(20),
			MaxNumberOfMessages: aws.Int64(10),
		})
		if err != nil {
			log.Println(err)
			time.Sleep(2 * time.Second)
		}
		for _, message := range resp.Messages {
			go func(msg *sqs.Message) {
				msgStruct := snsMessage{}
				json.Unmarshal([]byte(*msg.Body), &msgStruct)
				s.config.sqsClient.DeleteMessage(&sqs.DeleteMessageInput{
					QueueUrl:      aws.String(inbox),
					ReceiptHandle: msg.ReceiptHandle,
				})
			}(message)
		}
		log.WithField("event", "sat.sqs.inbox.close").Info()
	}
}

// NewSatellite creates a new satellite that handles pub sub. It provides a http.Handler
func NewSatellite(config SatelliteConfig) *Satellite {
	return &Satellite{
		antennas: make(map[string]*Antenna),
		config:   config,
	}
}
