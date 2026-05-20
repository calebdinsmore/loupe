// Package server wires the HTTP/WebSocket API to the git, store, and agent layers.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"github.com/calebjdinsmore/loupe/internal/adapter"
	"github.com/calebjdinsmore/loupe/internal/agent"
	"github.com/calebjdinsmore/loupe/internal/git"
	"github.com/calebjdinsmore/loupe/internal/store"
)

type Server struct {
	git    *git.Service
	store  *store.Store
	runner *agent.Runner
	mux    *chi.Mux

	mu   sync.Mutex
	hubs map[int64]*hub
}

func New(g *git.Service, st *store.Store, r *agent.Runner) *Server {
	s := &Server{git: g, store: st, runner: r, hubs: map[int64]*hub{}}
	m := chi.NewRouter()
	m.Get("/api/branches", s.handleBranches)
	m.Get("/api/diff", s.handleDiff)
	m.Post("/api/reviews", s.handleCreateReview)
	m.Get("/api/reviews/{id}/ws", s.handleWS)
	m.Handle("/*", spaHandler())
	s.mux = m
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleBranches(w http.ResponseWriter, _ *http.Request) {
	branches, err := s.git.Branches()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"branches": branches,
		"base":     s.git.DefaultBase(),
		"current":  s.git.CurrentBranch(),
	})
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	diff, err := s.git.Diff(r.URL.Query().Get("base"), r.URL.Query().Get("branch"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"diff": diff})
}

type createReviewReq struct {
	Branch   string          `json:"branch"`
	Base     string          `json:"base"`
	Mode     string          `json:"mode"`
	Comments []store.Comment `json:"comments"`
}

func (s *Server) handleCreateReview(w http.ResponseWriter, r *http.Request) {
	var req createReviewReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := s.store.CreateReview(req.Branch, req.Base, req.Mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, c := range req.Comments {
		c.ReviewID = id
		_ = s.store.AddComment(c)
	}
	go s.runReview(id)
	writeJSON(w, map[string]int64{"id": id})
}

// runReview builds the prompt, runs the agent, and fans events out to the hub.
func (s *Server) runReview(id int64) {
	h := s.hubFor(id)
	rev, err := s.store.Review(id)
	if err != nil {
		h.broadcast(agent.Event{Type: agent.EventError, Text: err.Error()})
		return
	}
	comments, _ := s.store.Comments(id)
	diff, _ := s.git.Diff(rev.Base, rev.Branch)

	prompt := adapter.BuildPrompt(rev, comments, diff)
	tools := adapter.AllowedTools(adapter.Mode(rev.Mode))

	_ = s.store.SetStatus(id, "running")
	events, err := s.runner.Run(context.Background(), prompt, tools, rev.SessionID)
	if err != nil {
		h.broadcast(agent.Event{Type: agent.EventError, Text: err.Error()})
		_ = s.store.SetStatus(id, "error")
		return
	}
	for ev := range events {
		if ev.Type == agent.EventSystem && ev.SessionID != "" {
			_ = s.store.SetSession(id, ev.SessionID)
		}
		h.broadcast(ev)
	}
	_ = s.store.SetStatus(id, "done")
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()

	h := s.hubFor(id)
	ch := h.subscribe()
	defer h.unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			if wsjson.Write(ctx, c, ev) != nil {
				return
			}
		}
	}
}

func (s *Server) hubFor(id int64) *hub {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.hubs[id]
	if !ok {
		h = newHub()
		s.hubs[id] = h
	}
	return h
}
