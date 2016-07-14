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

type satellite struct {
	channels map[string]*strobe.Strobe
	sync.RWMutex
	redisPool *redis.Pool
	queueURL  string
	sqsClient *sqs.SQS
}

func (s *satellite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	channelName := strings.Trim(r.URL.Path, "/")
	s.Lock()
	channel, ok := s.channels[channelName]
	if !ok {
		channel = strobe.NewStrobe()
		s.channels[channelName] = channel
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

	listener := channel.Listen()
	defer listener.Close()
	log.Println("Ah, a customer! on", channelName)
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
			log.Println("DING!")
			sse.Encode(w, sse.Event{
				Id:    strconv.Itoa(int(time.Now().UnixNano())),
				Event: "message",
				Data:  m,
			})
			flusher.Flush()
		case <-closer.CloseNotify():
			log.Println("Closed, ah?")
			return
		case <-time.After(10 * time.Second):
			log.Println("Sending a ping")
			sse.Encode(w, sse.Event{
				Id:    strconv.Itoa(int(time.Now().UnixNano())),
				Event: "heartbeat",
				Data:  "PING",
			})
			flusher.Flush()
		case <-killSwitch:
			log.Println("DEAD, ah?")
			return
		}
	}
}

func (s *satellite) startRedisStrobe() {
	r := s.redisPool.Get()
	defer r.Close()
	psc := redis.PubSubConn{Conn: r}
	psc.PSubscribe("*")
	defer psc.Close()
	for {
		switch n := psc.Receive().(type) {
		case redis.PMessage:
			log.Println("INGOMING!", n.Channel, string(n.Data))
			s.RLock()
			channel, ok := s.channels[n.Channel]
			s.RUnlock()
			if ok {
				log.Println("Heading to ", channel.Count())
				channel.Pulse(string(n.Data))
			}
			log.Println("Hmm.", channel, ok)
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

func (s *satellite) startSqsReceive() {
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

// NewSatelliteHandler creates a new handler that handles pub sub
func newSatelliteHandler(pool *redis.Pool, sqsClient *sqs.SQS, queueURL string) *satellite {
	s := &satellite{
		channels:  make(map[string]*strobe.Strobe),
		redisPool: pool,
		queueURL:  queueURL,
		sqsClient: sqsClient,
	}
	return s
}
