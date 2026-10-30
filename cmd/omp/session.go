package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"onboardmeplease/internal/engine"
	"strings"
)

type sessionRequest struct {
	Command   string `json:"command"`
	Query     string `json:"query,omitempty"`
	ID        string `json:"id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Direction string `json:"direction,omitempty"`
	Kinds     string `json:"kinds,omitempty"`
	Depth     int    `json:"depth,omitempty"`
	Nodes     int    `json:"nodes,omitempty"`
	Bytes     int    `json:"bytes,omitempty"`
	Cursor    string `json:"cursor,omitempty"`
}

// Each request gets a separate SQLite read transaction. A session reuses only
// the inventory audit; it does not retain an unbounded database snapshot.
func serveSession(s *engine.Store, input io.Reader, output io.Writer, rediscover func() (engine.Workspace, error)) error {
	enc := json.NewEncoder(output)
	enc.SetEscapeHTML(false)
	if err := sessionRefresh(s, true); err != nil {
		return err
	}
	if err := enc.Encode(s.SessionResult(s.Envelope("current", map[string]any{"ready": true}))); err != nil {
		return err
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	for scanner.Scan() {
		var req sessionRequest
		dec := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			if err = enc.Encode(sessionError("invalid_request")); err != nil {
				return err
			}
			continue
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			if err := enc.Encode(sessionError("invalid_request")); err != nil {
				return err
			}
			continue
		}
		if req.Command == "close" {
			return enc.Encode(s.SessionResult(s.Envelope("current", map[string]any{"closed": true})))
		}
		if req.Command == "refresh" {
			if rediscover != nil {
				w, err := rediscover()
				if err != nil {
					s.InvalidateSession()
					if err = enc.Encode(sessionError(err.Error())); err != nil {
						return err
					}
					continue
				}
				s.W = w
			}
			if err := sessionRefresh(s, false); err != nil {
				if err = enc.Encode(sessionError(err.Error())); err != nil {
					return err
				}
				continue
			}
			if err := enc.Encode(s.SessionResult(s.Envelope("current", map[string]any{"refreshed": true}))); err != nil {
				return err
			}
			continue
		}
		r, err := sessionQuery(s, req)
		if err != nil {
			r = sessionError(err.Error())
		} else {
			r = s.SessionResult(r)
		}
		if err = enc.Encode(r); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func sessionRefresh(s *engine.Store, first bool) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	s.Q = tx
	defer func() { s.Q = s.DB; tx.Rollback() }()
	if first {
		return s.BeginSession()
	}
	return s.RefreshSession()
}

func sessionError(msg string) engine.Envelope {
	return engine.Envelope{Schema: 1, State: "unavailable", Reasons: []string{strings.SplitN(msg, ":", 2)[0]}, Warnings: []string{}, Data: map[string]string{"error": msg}}
}

func sessionQuery(s *engine.Store, q sessionRequest) (engine.Envelope, error) {
	empty := engine.Envelope{}
	if q.Command != "investigate" && q.Command != "search" && q.Command != "navigate" && q.Command != "evidence" {
		return empty, fmt.Errorf("invalid_command")
	}
	if q.Limit < 0 || q.Limit > 100 || q.Depth < 0 || q.Depth > 8 || q.Nodes < 0 || q.Nodes > 200 || q.Bytes < 0 || q.Bytes > 1<<20 {
		return empty, fmt.Errorf("invalid_limit")
	}
	if q.Direction != "" && q.Direction != "both" && q.Direction != "incoming" && q.Direction != "outgoing" {
		return empty, fmt.Errorf("invalid_direction")
	}
	if q.Direction == "" {
		q.Direction = "both"
	}
	if q.Limit == 0 {
		q.Limit = 10
		if q.Command == "investigate" {
			q.Limit = 3
		}
	}
	if q.Depth == 0 {
		q.Depth = 1
	}
	if q.Nodes == 0 {
		q.Nodes = 20
	}
	if q.Bytes == 0 {
		q.Bytes = 65536
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return empty, err
	}
	s.Q = tx
	defer func() { s.Q = s.DB; tx.Rollback() }()
	if fresh, err := s.Audit(); err != nil {
		return empty, err
	} else if !fresh {
		r := s.Envelope("stale", nil)
		r.Reasons = append(r.Reasons, "session_generation_changed_or_refresh_failed")
		return r, nil
	}
	switch q.Command {
	case "investigate":
		return s.Investigate(q.Query, q.Limit)
	case "search":
		return s.Search(q.Query, q.Limit)
	case "navigate":
		return s.Navigate(q.Query, q.Limit, q.Direction, q.Kinds, q.Depth, q.Nodes, q.Bytes)
	case "evidence":
		return s.Evidence(q.ID, q.Direction, q.Kinds, q.Depth, q.Nodes, q.Bytes, q.Cursor)
	}
	return empty, fmt.Errorf("invalid_command")
}
