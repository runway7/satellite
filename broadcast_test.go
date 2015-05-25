package main

import (
	"bufio"
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPubsub(t *testing.T) {
        testPool := newPool("localhost:6379")
	monolithServer := httptest.NewServer(http.HandlerFunc(NewBroadcastHandler(testPool)))
	url := monolithServer.URL + "/channel"
	success := make(chan bool)
	group := &sync.WaitGroup{}
	for i := 1; i < 100; i++ {
		group.Add(1)

		go func(t *testing.T, group *sync.WaitGroup) {
			resp, err := http.Get(url)
			if err != nil {
				t.Error("should have been able to make the connection")
			}
			defer resp.Body.Close()
			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadBytes('\n')
				line = bytes.TrimSpace(line)
				t.Log("Received SSE message message ", string(line))
				if err != nil {
					break
				}
				if strings.Contains(string(line), "PING") {
					group.Done()
				}
			}
			if err != nil {
				t.Error("Shouldn't be an error")
			}
		}(t, group)
	}

	go func(group *sync.WaitGroup) {
		group.Wait()
		success <- true
	}(group)

	go func() {
		time.Sleep(time.Second / 100)
		http.Post(url, "text", nil)
	}()

	select {
	case <-success:
	case <-time.After(3 * time.Second):
		t.Error("No message received")
	}
}
