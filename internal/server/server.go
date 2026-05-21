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
	m.Get("/api/reviews", s.handleListReviews)
	m.Post("/api/reviews", s.handleEnsureReview)
	m.Get("/api/reviews/{id}/comments", s.handleListComments)
	m.Post("/api/reviews/{id}/comments", s.handleAddComment)
	m.Post("/api/reviews/{id}/submit", s.handleSubmitReview)
	m.Get("/api/reviews/{id}/ws", s.handleWS)
	m.Patch("/api/comments/{id}", s.handleUpdateComment)
	m.Delete("/api/comments/{id}", s.handleDeleteComment)
	m.Handle("/*", spaHandler())
	s.mux = m
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// pathID parses the {id} path param. On a non-numeric id it writes a 400 and
// returns ok=false so callers early-return rather than hitting the store with a
// silent 0 (which reads as a missing row).
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

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

// diffFor returns the diff for a base/branch pair, dispatching the working-tree
// sentinel to DiffWorking so committed + uncommitted changes show together.
// Shared by handleDiff and runReview so the live view and the agent prompt see
// the same diff.
func (s *Server) diffFor(base, branch string) (string, error) {
	if branch == git.WorkingRef {
		return s.git.DiffWorking(base)
	}
	return s.git.Diff(base, branch)
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	diff, err := s.diffFor(r.URL.Query().Get("base"), r.URL.Query().Get("branch"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"diff": diff})
}

// reviewJSON is a review plus its comments, used to hydrate the frontend on load.
type reviewJSON struct {
	store.Review
	Comments []store.Comment `json:"comments"`
}

// handleListReviews returns stored reviews (optionally filtered by ?branch=&base=),
// each with its comments embedded so the frontend can restore state in one call.
func (s *Server) handleListReviews(w http.ResponseWriter, r *http.Request) {
	branch := r.URL.Query().Get("branch")
	base := r.URL.Query().Get("base")
	reviews, err := s.store.Reviews(branch, base)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]reviewJSON, 0, len(reviews))
	for _, rev := range reviews {
		comments, err := s.store.Comments(rev.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, reviewJSON{Review: rev, Comments: comments})
	}
	writeJSON(w, map[string]any{"reviews": out})
}

type ensureReviewReq struct {
	Branch string `json:"branch"`
	Base   string `json:"base"`
	Mode   string `json:"mode"`
}

// handleEnsureReview returns the id of the review for a branch/base pair, creating
// a draft one if needed. It does not run the agent — that happens on submit.
func (s *Server) handleEnsureReview(w http.ResponseWriter, r *http.Request) {
	var req ensureReviewReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Branch == "" || req.Base == "" {
		http.Error(w, "branch and base are required", http.StatusBadRequest)
		return
	}
	mode := req.Mode
	if mode == "" {
		mode = string(adapter.ModeDocument)
	}
	id, err := s.store.EnsureReview(req.Branch, req.Base, mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int64{"id": id})
}

// handleListComments serves GET /api/reviews/{id}/comments. The SPA hydrates
// comments via listReviews instead, but this stays as deliberate, standalone
// API surface for scripts/debugging.
func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	comments, err := s.store.Comments(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"comments": comments})
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var c store.Comment
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.ReviewID = id
	newID, err := s.store.AddComment(c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	c.ID = newID
	writeJSON(w, c)
}

// updateCommentReq carries partial edits; nil fields are left unchanged.
type updateCommentReq struct {
	Body      *string `json:"body"`
	Submitted *bool   `json:"submitted"`
	Collapsed *bool   `json:"collapsed"`
}

func (s *Server) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req updateCommentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c, err := s.store.CommentByID(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if req.Body != nil {
		c.Body = *req.Body
	}
	if req.Submitted != nil {
		c.Submitted = *req.Submitted
	}
	if req.Collapsed != nil {
		c.Collapsed = *req.Collapsed
	}
	if err := s.store.UpdateComment(id, c.Body, c.Submitted, c.Collapsed); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, c)
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteComment(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type submitReviewReq struct {
	Mode string `json:"mode"`
}

// handleSubmitReview marks the review's pending comments as submitted and kicks
// off the agent run on that batch.
func (s *Server) handleSubmitReview(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req submitReviewReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Mode != "" {
		if err := s.store.SetMode(id, req.Mode); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	batch, err := s.store.MarkSubmitted(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(batch) == 0 {
		http.Error(w, "no pending comments to submit", http.StatusBadRequest)
		return
	}
	go s.runReview(id, batch)
	writeJSON(w, map[string]any{"id": id})
}

// runReview builds the prompt from the just-submitted batch, runs the agent, and
// fans events out to the hub.
func (s *Server) runReview(id int64, batch []store.Comment) {
	h := s.hubFor(id)
	rev, err := s.store.Review(id)
	if err != nil {
		h.broadcast(agent.Event{Type: agent.EventError, Text: err.Error()})
		return
	}
	diff, _ := s.diffFor(rev.Base, rev.Branch)

	prompt := adapter.BuildPrompt(rev, batch, diff)
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
	id, ok := pathID(w, r)
	if !ok {
		return
	}
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
