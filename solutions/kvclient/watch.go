package kvclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Stage 20 --------------------------------------------------------------------

// Event is one change reported by Watch.
type Event struct {
	ID    string // the SSE "id:" field
	Type  string // "put" or "delete"
	Key   string
	Value []byte // nil for deletes
}

// Watch streams changes to keys starting with prefix, calling fn for each
// one. It returns when ctx is cancelled (ctx.Err()), when fn returns an error
// (that error), or when the server ends the stream (nil).
func (c *Client) Watch(ctx context.Context, prefix string, fn func(Event) error) error {
	u := c.base.JoinPath("watch")
	u.RawQuery = url.Values{"prefix": {prefix}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", userAgent)

	// NOTE: if c.hc has a Timeout, it applies to the whole stream, body
	// included, and will cut a long watch short. Context cancellation is the
	// right way to bound a stream's lifetime.
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	// Closing the body is also how we abort the stream early (when fn fails).
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return readAPIError(resp)
	}

	err = readSSE(resp.Body, func(f sseFields) error {
		ev := Event{ID: f.id, Type: f.event}
		var payload struct {
			Key   string  `json:"key"`
			Value *string `json:"value"`
		}
		if err := json.Unmarshal([]byte(f.data), &payload); err != nil {
			return fmt.Errorf("kvclient: bad event data %q: %w", f.data, err)
		}
		ev.Key = payload.Key
		if payload.Value != nil {
			ev.Value = []byte(*payload.Value)
		}
		return fn(ev)
	})
	if ctx.Err() != nil {
		// Reading failed because we cancelled; report that, not the
		// resulting network error.
		return ctx.Err()
	}
	return err
}

type sseFields struct {
	id, event, data string
}

// readSSE parses a text/event-stream (the subset Watch needs) and calls
// dispatch for each complete event. See
// https://html.spec.whatwg.org/multipage/server-sent-events.html
func readSSE(r io.Reader, dispatch func(sseFields) error) error {
	sc := bufio.NewScanner(r)
	// bufio.Scanner's default limit is 64 KiB per line; values can be 1 MiB.
	sc.Buffer(make([]byte, 64<<10), 2<<20)

	var (
		cur     sseFields
		data    []string
		hasData bool
	)
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" {
			// A blank line ends the event.
			if hasData {
				cur.data = strings.Join(data, "\n")
				if cur.event == "" {
					cur.event = "message"
				}
				if err := dispatch(cur); err != nil {
					return err
				}
			}
			cur, data, hasData = sseFields{id: cur.id}, nil, false
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // a comment, e.g. a keepalive
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			cur.event = value
		case "data":
			data = append(data, value)
			hasData = true
		case "id":
			cur.id = value
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
