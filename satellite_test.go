package main

import (
	"math/rand"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/donovanhide/eventsource"
)

func runTestOnChannel(t *testing.T, channelURL, channel string, satellite *Satellite) chan bool {
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
		go satellite.Publish(channel, message)
	}()

	return finished
}

func TestSatellite(t *testing.T) {
	rand.Seed(time.Now().UnixNano())
	satellite := NewSatellite("test-outbox", "test-bucket")
	satelliteServer := httptest.NewServer(satellite)
	baseURL := satelliteServer.URL
	testCount := rand.Intn(10) + 1
	testWaits := &sync.WaitGroup{}
	testWaits.Add(testCount)
	for i := 0; i < testCount; i++ {
		testURL := baseURL + "/" + strconv.Itoa(i)
		finished := runTestOnChannel(t, testURL, strconv.Itoa(i), satellite)
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
