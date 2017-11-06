package main

import (
	"net/http"
	"sync"
	"github.com/manucorporat/sse"
)

type Beam struct {
	http.ResponseWriter
}

func (b *Beam) Pulse(event sse.Event) {
	sse.Encode(b, event)
	b.ResponseWriter.(http.Flusher).Flush()
}

type Antenna struct {
	beams map[Beam]struct{}
	sync.RWMutex
}

func NewAntenna() *Antenna {
	return &Antenna{beams: make(map[Beam]struct{})}
}

func (a *Antenna) Pulse(event sse.Event) {
	a.RLock()
	for beam := range a.beams {
		sse.Encode(beam, event)
		beam.ResponseWriter.(http.Flusher).Flush()
	}
	a.RUnlock()
}

func (a *Antenna) Add(rw http.ResponseWriter) Beam {
	beam := Beam{rw}
	beam.Header().Set("Content-Type", "text/event-stream")
	beam.Header().Set("Cache-Control", "no-cache")
	beam.Header().Set("Connection", "keep-alive")
	beam.Pulse(sse.Event{
		Event: "open",
		Data:  "START",
	})
	a.Lock()
	a.beams[beam] = struct{}{}
	a.Unlock()
	return beam
}

func (a *Antenna) Remove(beam Beam) {
	a.Lock()
	delete(a.beams, beam)
	a.Unlock()
}

func (a *Antenna) Size() int {
	a.RLock()
	defer a.RUnlock()
	return len(a.beams)
}
