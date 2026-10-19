package gobin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Stage 19 --------------------------------------------------------------------

// stream writes n lines of JSON, flushing after each one, waiting
// ?interval= between lines.
func (a *app) stream(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 || n > 100 {
		writeError(w, http.StatusBadRequest, "n must be an integer between 1 and 100")
		return
	}
	var interval time.Duration
	if s := r.URL.Query().Get("interval"); s != "" {
		interval, err = time.ParseDuration(s)
		if err != nil || interval < 0 || interval > 5*time.Second {
			writeError(w, http.StatusBadRequest, "interval must be a duration between 0 and 5s")
			return
		}
	}

	// ResponseController finds Flush even through middleware wrappers that
	// implement Unwrap. A plain w.(http.Flusher) assertion would not.
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	for i := range n {
		if i > 0 && interval > 0 {
			select {
			case <-time.After(interval):
			case <-r.Context().Done():
				return
			case <-a.shutdown:
				return
			}
		}
		if err := enc.Encode(map[string]any{"id": i, "time": time.Now().UTC()}); err != nil {
			return
		}
		// Without Flush the line would sit in net/http's 4 KiB buffer until
		// the handler returns.
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

// subscribe registers a watcher for keys with the given prefix.
func (s *store) subscribe(prefix string) *subscriber {
	sub := &subscriber{prefix: prefix, ch: make(chan event, 64)}
	s.mu.Lock()
	s.subs[sub] = struct{}{}
	s.mu.Unlock()
	return sub
}

// unsubscribe removes sub. Safe to call after the store already dropped it.
func (s *store) unsubscribe(sub *subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[sub]; ok {
		delete(s.subs, sub)
		close(sub.ch)
	}
}

// publishLocked delivers ev to every interested watcher. The caller holds
// s.mu. It never blocks: a watcher whose buffer is full is too slow to keep
// up, so it is dropped (its channel closed, which ends its stream) rather
// than being allowed to stall every writer.
func (s *store) publishLocked(ev event) {
	s.seq++
	ev.ID = s.seq
	for sub := range s.subs {
		if !strings.HasPrefix(ev.Key, sub.prefix) {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			delete(s.subs, sub)
			close(sub.ch)
		}
	}
}

// watch streams store changes as Server-Sent Events.
func (a *app) watch(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	// A watch may stay open far longer than the server's WriteTimeout.
	// Lift the deadline for this response only. (Not every ResponseWriter
	// supports it, e.g. httptest.ResponseRecorder; that's fine.)
	_ = rc.SetWriteDeadline(time.Time{})

	// Subscribe before sending headers: once the client sees the headers it
	// may assume it won't miss any changes.
	sub := a.kv.subscribe(r.URL.Query().Get("prefix"))
	defer a.kv.unsubscribe(sub)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return
	}

	// Comments keep idle connections alive through proxies that time out
	// silent connections.
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case ev, ok := <-sub.ch:
			if !ok {
				return // dropped for being too slow
			}
			if err := writeEvent(w, ev); err != nil {
				return
			}
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
		case <-r.Context().Done():
			return // client went away
		case <-a.shutdown:
			return // server shutting down (stage 23)
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

// writeEvent writes one SSE event: "id:", "event:" and "data:" lines, then a
// blank line that tells the client the event is complete.
func writeEvent(w http.ResponseWriter, ev event) error {
	payload := map[string]string{"key": ev.Key}
	if ev.Type == "put" {
		payload["value"] = string(ev.Value)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, data)
	return err
}
