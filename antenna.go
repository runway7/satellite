package main

import (
	"io"
	"net/http"
	"sync"

	"github.com/manucorporat/sse"
)

type Beam struct {
	io.Writer
}

func (b *Beam) Pulse(event sse.Event) {
	sse.Encode(b, event)
	flusher, ok := b.Writer.(http.Flusher)
	if ok {
		flusher.Flush()
	}
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
		beam.Pulse(event)
	}
	a.RUnlock()
}

func (a *Antenna) Add(rw http.ResponseWriter) Beam {
	beam := Beam{rw}
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
