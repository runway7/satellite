package main

import (
	"math/rand"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/donovanhide/eventsource"
	"github.com/garyburd/redigo/redis"
)

func makeTestServer(testPool *redis.Pool) *httptest.Server {
	handler := NewSatellite(testPool, newSQSClient(), "q1")
	go handler.StartRedisListener()
	return httptest.NewServer(handler)
}

func runTestOnChannel(t *testing.T, channelURL, channel string, testPool *redis.Pool) chan bool {
	finished := make(chan bool)
	waits := &sync.WaitGroup{}
	subscriberCount := rand.Intn(20) + 1
	subscriberWaits := &sync.WaitGroup{}
	subscriberWaits.Add(subscriberCount)
	message := strconv.Itoa(subscriberCount)
	for i := 0; i < subscriberCount; i++ {
		waits.Add(1)
		go func() {
			stream, err := eventsource.Subscribe(channelURL, "")
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for {
					e := <-stream.Events
					if e.Data() == message {
						waits.Done()
					}
				}
			}()
			time.Sleep(1 * time.Second)
			subscriberWaits.Done()
		}()
	}

	go func() {
		waits.Wait()
		finished <- true
	}()

	go func() {
		subscriberWaits.Wait()
		conn := testPool.Get()
		conn.Do("PUBLISH", channel, message)
		conn.Close()
	}()

	return finished
}

func TestSatellite(t *testing.T) {
	rand.Seed(time.Now().UnixNano())
	testPool := newPool("localhost:6379")
	satelliteServer := makeTestServer(testPool)
	baseURL := satelliteServer.URL
	testCount := rand.Intn(10) + 1
	testWaits := &sync.WaitGroup{}
	testWaits.Add(testCount)
	for i := 0; i < testCount; i++ {
		testURL := baseURL + "/" + strconv.Itoa(i)
		finished := runTestOnChannel(t, testURL, strconv.Itoa(i), testPool)
		go func(f chan bool, w *sync.WaitGroup) {
			<-f
			w.Done()
		}(finished, testWaits)
	}
	success := make(chan bool)
	go func() {
		testWaits.Wait()
		success <- true
	}()
	select {
	case <-success:
	case <-time.After(5 * time.Second):
		t.Error("No message received")
	}
}
