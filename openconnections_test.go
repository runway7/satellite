package main

import (
	"bufio"
	"bytes"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestOpenConnections(t *testing.T) {
	runtime.GOMAXPROCS(runtime.NumCPU())
	testPool := newPool("localhost:6379")
	monolithServer := httptest.NewServer(http.HandlerFunc(NewOpenConnectionsHandler(testPool)))
	testUrl := monolithServer.URL + "/openconnections"
	success := make(chan bool)
	group := &sync.WaitGroup{}
	for i := 1; i < 100; i++ {
		group.Add(1)

		go func(t *testing.T, group *sync.WaitGroup) {
			resp, err := http.Get(testUrl)
			if err != nil {
				t.Error("should have been able to make the connection")
			}
			defer resp.Body.Close()
			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadBytes('\n')
				line = bytes.TrimSpace(line)
				t.Log("Received number of connections ", string(line))
				if err != nil {
					break
				}
				group.Done()
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

	select {
	case <-success:
	case <-time.After(3 * time.Second):
		t.Error("Couldn't get number of connections")
	}
}
