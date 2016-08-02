package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sns"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/garyburd/redigo/redis"
	"github.com/manucorporat/sse"
	"github.com/sudhirj/strobe"
)

// Satellite is a broadcaster that provides a http.Handler
type Satellite struct {
	topics map[string]*strobe.Strobe
	sync.RWMutex
	redisPool    *redis.Pool
	sqsClient    *sqs.SQS
	snsClient    *sns.SNS
	s3Client     *s3.S3
	outbox       string
	configBucket string
}

func (s *Satellite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		topicName := strings.Trim(r.URL.Path, "/")
		s.Lock()
		topic, ok := s.topics[topicName]
		if !ok {
			topic = strobe.NewStrobe()
			s.topics[topicName] = topic
		}
		s.Unlock()

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		closer, ok := w.(http.CloseNotifier)
		if !ok {
			http.Error(w, "Closing unsupported!", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		listener := topic.Listen()
		defer listener.Close()
		sse.Encode(w, sse.Event{
			Id:    strconv.Itoa(int(time.Now().UnixNano())),
			Event: "open",
			Data:  "START",
		})
		flusher.Flush()
		killSwitch := time.After(5 * time.Minute)

		for {
			select {
			case m := <-listener.Receiver():
				sse.Encode(w, sse.Event{
					Id:    strconv.Itoa(int(time.Now().UnixNano())),
					Event: "message",
					Data:  m,
				})
				flusher.Flush()

			case <-closer.CloseNotify():
				return

			case <-time.After(20 * time.Second):
				currentTime := strconv.Itoa(int(time.Now().UnixNano()))
				sse.Encode(w, sse.Event{
					Id:    currentTime,
					Event: "heartbeat",
					Data:  currentTime,
				})
				flusher.Flush()

			case <-killSwitch:
				return
			}
		}
	}

	if r.Method == "POST" {
		path := strings.Trim(r.URL.Path, "/")
		pathComponents := strings.Split(path, "/")
		realm := pathComponents[0]
		config, err := s.loadRealmConfig(realm)
		if err != nil {
			http.Error(w, "Realm not recognized", http.StatusNotFound)
			return
		}
		if config.Password != r.FormValue("token") {
			http.Error(w, "Wrong token", http.StatusForbidden)
			return
		}

		message, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Could not read message", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		_, err = s.snsClient.Publish(&sns.PublishInput{
			Subject:  aws.String(path),
			Message:  aws.String(string(message)),
			TopicArn: aws.String(s.outbox),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	}

}

type realmConfig struct {
	Password string `json:"password"`
}

func (s *Satellite) loadRealmConfig(realm string) (realmConfig, error) {
	log.Println("loading from ", s.configBucket)
	configResponse, err := s.s3Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.configBucket),
		Key:    aws.String(realm),
	})

	if err != nil {
		log.Println(err)
		return realmConfig{}, err
	}

	configBytes, err := ioutil.ReadAll(configResponse.Body)
	defer configResponse.Body.Close()
	if err != nil {
		log.Println(err)
		return realmConfig{}, err
	}

	config := realmConfig{}

	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		log.Println(err)
		return realmConfig{}, err
	}

	return config, nil
}

// Publish sends the given message to all subscribers of the given topic
func (s *Satellite) Publish(topic, message string) {
	s.RLock()
	channel, ok := s.topics[topic]
	s.RUnlock()
	if ok {
		channel.Pulse(message)
	}
}

// SetRedisPool sets the redis pool
func (s *Satellite) SetRedisPool(pool *redis.Pool) {
	s.redisPool = pool
}

// SetSQSClient sets the SQS client for use as the listener
func (s *Satellite) SetSQSClient(client *sqs.SQS) {
	s.sqsClient = client
}

// SetS3Client sets the S3 client for use as the listener
func (s *Satellite) SetS3Client(client *s3.S3) {
	s.s3Client = client
}

// SetSNSClient sets the SNS client for use as the listener
func (s *Satellite) SetSNSClient(client *sns.SNS) {
	s.snsClient = client
}

// StartRedisListener subscribes to Redis events and broadcasts them to
// connected clients
func (s *Satellite) StartRedisListener() {
	r := s.redisPool.Get()
	defer r.Close()
	psc := redis.PubSubConn{Conn: r}
	psc.PSubscribe("*")
	defer psc.Close()
	for {
		switch n := psc.Receive().(type) {
		case redis.PMessage:
			go s.Publish(n.Channel, string(n.Data))
		case error:
			log.Printf("error: %v\n", n)
			return
		}
	}
}

type snsMessage struct {
	Channel string `json:"Subject"`
	Data    string `json:"Message"`
}

// StartSQSListener listens for SQS messages and publishes them to Redis
func (s *Satellite) StartSQSListener(inbox string) {
	for {
		log.Println("Opening receive...")
		resp, err := s.sqsClient.ReceiveMessage(&sqs.ReceiveMessageInput{
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
				log.Println(msgStruct)
				conn := s.redisPool.Get()
				conn.Do("PUBLISH", msgStruct.Channel, msgStruct.Data)
				conn.Close()
				s.sqsClient.DeleteMessage(&sqs.DeleteMessageInput{
					QueueUrl:      aws.String(inbox),
					ReceiptHandle: msg.ReceiptHandle,
				})
			}(message)
		}
		log.Println("Receive ended.")
	}
}

// NewSatellite creates a new satellite that handles pub sub. It provides a http.Handler
func NewSatellite(outbox, configBucket string) *Satellite {
	return &Satellite{
		topics:       make(map[string]*strobe.Strobe),
		outbox:       outbox,
		configBucket: configBucket,
	}
}
