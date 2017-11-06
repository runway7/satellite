package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sns"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/garyburd/redigo/redis"
	"github.com/manucorporat/sse"
)

// Satellite is a broadcaster that provides a http.Handler
type Satellite struct {
	antennas     map[string]*Antenna
	antennaMutex sync.RWMutex
	redisPool    *redis.Pool
	sqsClient    *sqs.SQS
	snsClient    *sns.SNS
	s3Client     *s3.S3
	outbox       string
	configBucket string
}

type topic struct {
	path           string
	pathComponents []string
	realmID        string
	topicID        string
}
type realmConfig struct {
	Password string `json:"password"`
}

func parseTopic(path string) topic {
	path = strings.Trim(path, "/")
	pathComponents := strings.Split(path, "/")
	realmID := pathComponents[0]
	topicID := strings.Join(pathComponents[1:], "/")
	return topic{
		path:           path,
		pathComponents: pathComponents,
		realmID:        realmID,
		topicID:        topicID,
	}
}

func (s *Satellite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	descriptor := parseTopic(r.URL.Path)
	if r.Method == "GET" {
		s.listen(descriptor, w, r)
	}

	if r.Method == "POST" {
		s.post(descriptor, w, r)
	}
}
func (s *Satellite) post(descriptor topic, w http.ResponseWriter, r *http.Request) {
	config, err := s.loadRealmConfig(descriptor.realmID)
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

	response, err := s.snsClient.Publish(&sns.PublishInput{
		Subject:  aws.String(descriptor.path),
		Message:  aws.String(string(message)),
		TopicArn: aws.String(s.outbox),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.WithFields(log.Fields{
		"event": "sat.publish",
		"realm": descriptor.realmID,
		"topic": descriptor.topicID,
		"id":    *response.MessageId,
		"table": "publishes",
	}).Info()
}
func (s *Satellite) listen(descriptor topic, w http.ResponseWriter, r *http.Request) {
	s.antennaMutex.Lock()
	antenna, ok := s.antennas[descriptor.path]
	if !ok {
		antenna = NewAntenna()
		s.antennas[descriptor.path] = antenna
	}
	s.antennaMutex.Unlock()

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

	w.Header().Set("Access-Control-Allow-Origin", "*")

	sessionID := make([]byte, 18)
	rand.Read(sessionID)

	log.WithFields(log.Fields{
		"event":   "sat.connection.start",
		"id":      base64.StdEncoding.EncodeToString(sessionID) + "/b",
		"session": base64.StdEncoding.EncodeToString(sessionID),
		"realm":   descriptor.realmID,
		"topic":   descriptor.topicID,
		"ip":      r.RemoteAddr,
		"table":   "connections",
	}).Info()

	defer func() {
		log.WithFields(log.Fields{
			"event":   "sat.connection.stop",
			"id":      base64.StdEncoding.EncodeToString(sessionID) + "/e",
			"session": base64.StdEncoding.EncodeToString(sessionID),
			"realm":   descriptor.realmID,
			"topic":   descriptor.topicID,
			"ip":      r.RemoteAddr,
			"table":   "connections",
		}).Info()
	}()

	killSwitch := time.After(10 * time.Minute)
	beam := antenna.Add(w)
	defer antenna.Remove(beam)
	for {
		select {
		case <-closer.CloseNotify():
			return

		case <-time.After(45 * time.Second):
			currentTime := strconv.Itoa(int(time.Now().UnixNano()))
			beam.Pulse(sse.Event{
				Event: "ping",
				Data:  currentTime,
			})
		case <-killSwitch:
			return
		}
	}
}

func (s *Satellite) loadRealmConfig(realm string) (realmConfig, error) {
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
	s.antennaMutex.RLock()
	antenna, ok := s.antennas[topic]
	s.antennaMutex.RUnlock()
	if ok {
		go antenna.Pulse(sse.Event{
			Id:    strconv.Itoa(int(time.Now().UnixNano())),
			Event: "message",
			Data:  message,
		})
		topicParts := strings.Split(topic, "/")
		id := make([]byte, 18)
		rand.Read(id)
		go log.WithFields(log.Fields{
			"event": "sat.delivery",
			"realm": topicParts[0],
			"topic": strings.Join(topicParts[1:], "/"),
			"count": antenna.Size(),
			"table": "deliveries",
			"id":    base64.StdEncoding.EncodeToString(id),
		}).Info()
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
		log.WithField("event", "sat.sqs.inbox.open").Info()
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
				conn := s.redisPool.Get()
				conn.Do("PUBLISH", msgStruct.Channel, msgStruct.Data)
				conn.Close()
				s.sqsClient.DeleteMessage(&sqs.DeleteMessageInput{
					QueueUrl:      aws.String(inbox),
					ReceiptHandle: msg.ReceiptHandle,
				})
			}(message)
		}
		log.WithField("event", "sat.sqs.inbox.close").Info()
	}
}

// NewSatellite creates a new satellite that handles pub sub. It provides a http.Handler
func NewSatellite(outbox, configBucket string) *Satellite {
	return &Satellite{
		antennas:     make(map[string]*Antenna),
		outbox:       outbox,
		configBucket: configBucket,
	}
}
