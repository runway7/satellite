package main

import (
	"bufio"
	"bytes"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func makeTestServer() *httptest.Server {
	testPool := newPool("localhost:6379", "")
	token := "token"
	return httptest.NewServer(http.HandlerFunc(NewSatelliteHandler(testPool, token)))
}

func runTestOnChannel(t *testing.T, channelURL string) chan bool {
	finished := make(chan bool)
	waits := &sync.WaitGroup{}
	subscriberCount := rand.Intn(20)
	subscriberWaits := &sync.WaitGroup{}
	subscriberWaits.Add(subscriberCount)
	for i := 0; i < subscriberCount; i++ {
		waits.Add(1)
		go func(t *testing.T, group *sync.WaitGroup) {
			resp, err := http.Get(channelURL)
			if err != nil {
				t.Error("should have been able to make the connection")
			}
			defer resp.Body.Close()
			subscriberWaits.Done()
			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadBytes('\n')
				line = bytes.TrimSpace(line)
				if err != nil {
					break
				}
				if strings.Contains(string(line), "PONG") {
					group.Done()
				}
			}
		}(t, waits)
	}

	go func() {
		waits.Wait()
		finished <- true
	}()

	go func() {
		subscriberWaits.Wait()
		v := url.Values{}
		v.Set("token", "token")
		v.Add("message", "PING")
		http.PostForm(channelURL, v)
	}()

	return finished
}

func TestSatellite(t *testing.T) {
	satelliteServer := makeTestServer()
	testCount := rand.Intn(20)
	testWaits := &sync.WaitGroup{}
	testWaits.Add(testCount)
	for i := 0; i < testCount; i++ {
		testURL := satelliteServer.URL + "/" + string(i)
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
