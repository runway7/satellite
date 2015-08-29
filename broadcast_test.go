package main

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/donovanhide/eventsource"
)

func makeTestServer() *httptest.Server {
	testPool := newPool("localhost:6379", "")
	token := "token"
	return httptest.NewServer(http.HandlerFunc(NewSatelliteHandler(testPool, token)))
}

func runTestOnChannel(t *testing.T, channelURL string) chan bool {
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
			subscriberWaits.Done()
		}()
	}

	go func() {
		waits.Wait()
		finished <- true
	}()

	go func() {
		subscriberWaits.Wait()
		v := url.Values{}
		v.Set("token", "token")
		v.Set("message", message)
		http.PostForm(channelURL, v)
	}()

	return finished
}

func TestSatellite(t *testing.T) {
	rand.Seed(time.Now().UnixNano())
	satelliteServer := makeTestServer()
	baseURL := satelliteServer.URL
	testCount := rand.Intn(10) + 1
	testWaits := &sync.WaitGroup{}
	testWaits.Add(testCount)
	for i := 0; i < testCount; i++ {
		testURL := baseURL + "/" + strconv.Itoa(i)
		finished := runTestOnChannel(t, testURL)
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
