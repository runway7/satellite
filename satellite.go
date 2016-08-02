package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/garyburd/redigo/redis"
	"github.com/manucorporat/sse"
	"github.com/sudhirj/strobe"
)

// Satellite is a broadcaster that provides a http.Handler
type Satellite struct {
	topics map[string]*strobe.Strobe
	sync.RWMutex
	redisPool *redis.Pool
	queueURL  string
	sqsClient *sqs.SQS
}

func (s *Satellite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

// Publish sends the given message to all subscribers of the given topic
func (s *Satellite) Publish(topic, message string) {
	s.RLock()
	channel, ok := s.topics[topic]
	s.RUnlock()
	if ok {
		channel.Pulse(message)
	}
}

// StartRedisListener subscribes to Redis events and broadcasts them to
// connected clients
func (s *Satellite) StartRedisListener(pool *redis.Pool) {
	s.redisPool = pool
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
func (s *Satellite) StartSQSListener(sqsClient *sqs.SQS, queueURL string) {
	s.sqsClient = sqsClient
	s.queueURL = queueURL
	for {
		log.Println("Opening receive...")
		resp, err := s.sqsClient.ReceiveMessage(&sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(s.queueURL),
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
					QueueUrl:      aws.String(s.queueURL),
					ReceiptHandle: msg.ReceiptHandle,
				})
			}(message)
		}
		log.Println("Receive ended.")
	}
}

// NewSatellite creates a new satellite that handles pub sub. It provides a http.Handler
func NewSatellite() *Satellite {
	return &Satellite{
		topics: make(map[string]*strobe.Strobe),
	}
}
