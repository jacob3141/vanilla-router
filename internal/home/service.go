// SPDX-License-Identifier: AGPL-3.0-or-later

package home

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Target is an address in the home network that may be a device, with the name the router knows for it.
type Target struct {
	Host string
	Name string
}

const (
	pollEvery     = 5 * time.Second
	reidentify    = 10 * time.Minute // addresses that are no known device are asked again this rarely
	targetsEvery  = time.Minute
	requestTime   = 3 * time.Second
	maxConcurrent = 12
	maxReply      = 1 << 18
	offlineAfter  = 3 // failed rounds until a device counts as offline
)

type known struct {
	dev     Device
	missed  int
	nextTry time.Time // only for addresses that are no device
	isNot   bool
	auth    bool
}

// Service finds and reads devices.
type Service struct {
	Store   *Store
	Targets func() []Target
	Log     *slog.Logger
	HTTP    *http.Client // tests replace it
	Fake    []Device     // test instance: simulated devices, nothing is contacted

	mu    sync.RWMutex
	hosts map[string]*known
	gen   uint64
	cond  *sync.Cond
}

func (s *Service) init() {
	if s.HTTP == nil {
		s.HTTP = &http.Client{Timeout: requestTime, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport: &http.Transport{MaxIdleConnsPerHost: 1, IdleConnTimeout: 30 * time.Second}}
	}
	if s.Log == nil {
		s.Log = slog.New(slog.DiscardHandler)
	}
	s.hosts = map[string]*known{}
	s.cond = sync.NewCond(&sync.Mutex{})
}

func (s *Service) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("status %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, maxReply))
}

// probe asks one address who it is and reads it.
func (s *Service) probe(ctx context.Context, t Target, k *known) {
	now := time.Now()
	if k.dev.ID == "" {
		if k.isNot && now.Before(k.nextTry) {
			return
		}
		b, err := s.get(ctx, "http://"+t.Host+"/shelly")
		if err != nil {
			k.isNot, k.nextTry = true, now.Add(reidentify)
			return
		}
		adapter, id, model, fw, auth, err := parseIdentity(b)
		if err != nil {
			k.isNot, k.nextTry = true, now.Add(reidentify)
			return
		}
		k.isNot, k.auth = false, auth
		k.dev = Device{ID: id, Host: t.Host, Adapter: adapter, Model: model, Firmware: fw, Entities: []Entity{}}
	}
	path := "/rpc/Shelly.GetStatus"
	if k.dev.Adapter == adapterShelly1 {
		path = "/status"
	}
	b, err := s.get(ctx, "http://"+t.Host+path)
	var ents []Entity
	if err == nil {
		if k.dev.Adapter == adapterShelly1 {
			ents, err = parseGen1(b)
		} else {
			ents, err = parseGen2(b)
		}
	}
	if err != nil {
		k.missed++
		if k.missed >= offlineAfter {
			k.dev.Online = false
		}
		return
	}
	k.missed, k.dev.Online, k.dev.LastSeen, k.dev.Entities = 0, true, now, ents
}

// round reads everything once.
func (s *Service) round(ctx context.Context, targets []Target) {
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for _, t := range targets {
		s.mu.Lock()
		k := s.hosts[t.Host]
		if k == nil {
			k = &known{}
			s.hosts[t.Host] = k
		}
		s.mu.Unlock()
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// A copy, so that readers never see a half-written device
			s.mu.RLock()
			c := *k
			s.mu.RUnlock()
			s.probe(ctx, t, &c)
			c.dev.Name = firstOf(t.Name, t.Host)
			s.mu.Lock()
			*k = c
			s.mu.Unlock()
		}()
	}
	wg.Wait()
	s.bump()
}

func (s *Service) bump() {
	s.cond.L.Lock()
	s.gen++
	s.cond.L.Unlock()
	s.cond.Broadcast()
}

// Run reads the devices until ctx ends.
func (s *Service) Run(ctx context.Context) {
	s.init()
	if s.Fake != nil {
		s.runFake(ctx)
		return
	}
	var targets []Target
	var loaded time.Time
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		if time.Since(loaded) >= targetsEvery {
			targets, loaded = s.Targets(), time.Now()
		}
		s.round(ctx, targets)
		select {
		case <-ctx.Done():
			s.cond.Broadcast()
			return
		case <-t.C:
		}
	}
}

// runFake lets simulated values change a little, for the test instance.
func (s *Service) runFake(ctx context.Context) {
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for i := 0; ; i++ {
		s.mu.Lock()
		for j := range s.Fake {
			d := s.Fake[j]
			d.LastSeen = time.Now()
			d.Entities = slices.Clone(d.Entities)
			for n := range d.Entities {
				if p := d.Entities[n].Power; p != nil && *p > 0 {
					d.Entities[n].Power = floatp(*p + float64(i%5))
				}
			}
			s.hosts[d.ID] = &known{dev: d}
		}
		s.mu.Unlock()
		s.bump()
		select {
		case <-ctx.Done():
			s.cond.Broadcast()
			return
		case <-t.C:
		}
	}
}

// Overview merges found devices with the owner's arrangement.
func (s *Service) Overview() Overview {
	out := Overview{Rooms: s.Store.Rooms(), Devices: []Device{}}
	s.mu.RLock()
	for _, k := range s.hosts {
		if k.dev.ID != "" {
			out.Devices = append(out.Devices, k.dev)
		}
	}
	s.mu.RUnlock()
	for i := range out.Devices {
		d := &out.Devices[i]
		c := s.Store.Device(d.ID)
		d.Room = c.Room
		if c.Name != "" {
			d.Name, d.Renamed = c.Name, true
		}
	}
	slices.SortFunc(out.Devices, func(a, b Device) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, `{"error":"Invalid request."}`, http.StatusBadRequest)
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, err error) {
	if err == nil {
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	code, msg := http.StatusInternalServerError, "The change could not be saved."
	if errors.Is(err, ErrInvalid) {
		code, msg = http.StatusBadRequest, strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": ")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Handler is the interface for the web server, behind its login and CSRF checks.
func (s *Service) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /overview", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Overview()) })
	m.HandleFunc("GET /events", s.events)
	m.HandleFunc("POST /device", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Room string `json:"room"`
		}
		if !readJSON(w, r, &p) {
			return
		}
		writeResult(w, s.Store.SetDevice(p.ID, p.Name, p.Room))
		s.bump()
	})
	m.HandleFunc("POST /room", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if !readJSON(w, r, &p) {
			return
		}
		_, err := s.Store.SetRoom(p.ID, p.Name)
		writeResult(w, err)
		s.bump()
	})
	m.HandleFunc("POST /command", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Device string `json:"device"`
			Entity string `json:"entity"`
			Action string `json:"action"`
			Value  *int   `json:"value"`
		}
		if !readJSON(w, r, &p) {
			return
		}
		writeResult(w, s.Command(r.Context(), p.Device, p.Entity, p.Action, p.Value))
	})
	m.HandleFunc("POST /room/delete", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			ID string `json:"id"`
		}
		if !readJSON(w, r, &p) {
			return
		}
		writeResult(w, s.Store.DeleteRoom(p.ID))
		s.bump()
	})
	return m
}

// events sends the whole overview whenever a round has finished or the arrangement changed.
func (s *Service) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// Cond has no context: a closed connection ends the wait through a broadcast
	stop := context.AfterFunc(r.Context(), s.cond.Broadcast)
	defer stop()
	var last uint64
	var lastBody []byte
	for r.Context().Err() == nil {
		b, _ := json.Marshal(s.Overview())
		// LastSeen changes every round, it does not count as a change worth a redraw
		if !sameBody(b, lastBody) {
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			fl.Flush()
			lastBody = b
		}
		s.cond.L.Lock()
		for s.gen == last && r.Context().Err() == nil {
			s.cond.Wait()
		}
		last = s.gen
		s.cond.L.Unlock()
	}
}

func sameBody(a, b []byte) bool {
	return string(a) == string(b)
}
