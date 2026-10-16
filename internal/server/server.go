// Package server exposes the evaluator over HTTP and serves the embedded UI.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pia/porthole/internal/eval"
	"github.com/pia/porthole/internal/model"
	"github.com/pia/porthole/web"
)

// Source loads a snapshot.
type Source interface {
	Load(ctx context.Context) (model.Snapshot, error)
}

// Server serves the UI and the evaluation API.
type Server struct {
	source     Source
	sourceName string
	interval   time.Duration

	mu         sync.RWMutex
	snap       *model.Snapshot
	loadedAt   time.Time
	lastErr    string
	refreshGen uint64
}

// New builds a server. Call Refresh before Run so the first request has data.
func New(source Source, sourceName string, interval time.Duration) *Server {
	return &Server{source: source, sourceName: sourceName, interval: interval}
}

// Refresh replaces the snapshot. On failure the previous snapshot is kept.
// A slower refresh that started earlier does not overwrite a newer one.
func (s *Server) Refresh(ctx context.Context) error {
	s.mu.Lock()
	s.refreshGen++
	gen := s.refreshGen
	s.mu.Unlock()

	snap, err := s.source.Load(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.refreshGen {
		return err
	}
	if err != nil {
		s.lastErr = err.Error()
		return err
	}
	s.snap = &snap
	s.loadedAt = time.Now()
	s.lastErr = ""
	return nil
}

// Run refreshes in the background and serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context, addr string) error {
	go s.loop(ctx)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("Porthole listening on %s", addr)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) loop(ctx context.Context) {
	if s.interval <= 0 {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := s.Refresh(c)
			cancel()
			if err != nil {
				log.Printf("refresh: %v", err)
			}
		}
	}
}

// Handler is the HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(web.Files))))
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/meta", s.meta)
	mux.HandleFunc("GET /api/namespaces", s.namespaces)
	mux.HandleFunc("GET /api/pods", s.pods)
	mux.HandleFunc("GET /api/policies", s.policies)
	mux.HandleFunc("GET /api/pods/{namespace}/{name}", s.explain)
	mux.HandleFunc("POST /api/check", s.check)
	mux.HandleFunc("GET /api/graph", s.graph)
	mux.HandleFunc("POST /api/refresh", s.refresh)
	return mux
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	b, err := web.Files.ReadFile("index.html")
	if err != nil {
		http.Error(w, "missing ui", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	body := map[string]any{
		"source":   s.sourceName,
		"loadedAt": s.loadedAt,
	}
	if s.lastErr != "" {
		body["refreshError"] = s.lastErr
	}
	if s.snap != nil {
		body["namespaces"] = len(s.snap.Namespaces)
		body["pods"] = len(s.snap.Pods)
		body["policies"] = len(s.snap.Policies)
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) namespaces(w http.ResponseWriter, r *http.Request) {
	idx, err := s.current(r)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	type row struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	}
	out := make([]row, 0, len(idx.Snap.Namespaces))
	for _, ns := range idx.Snap.Namespaces {
		out = append(out, row{Name: ns.Name, Labels: ns.Labels})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) pods(w http.ResponseWriter, r *http.Request) {
	idx, err := s.current(r)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, idx.Pods(r.URL.Query().Get("namespace")))
}

func (s *Server) policies(w http.ResponseWriter, r *http.Request) {
	idx, err := s.current(r)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	ns := r.URL.Query().Get("namespace")
	type row struct {
		Namespace            string   `json:"namespace"`
		Name                 string   `json:"name"`
		PodSelector          string   `json:"podSelector"`
		PolicyTypes          []string `json:"policyTypes"`
		PolicyTypesDefaulted bool     `json:"policyTypesDefaulted"`
		IngressRules         int      `json:"ingressRules"`
		EgressRules          int      `json:"egressRules"`
	}
	var out []row
	for _, pol := range idx.Snap.Policies {
		if ns != "" && pol.Namespace != ns {
			continue
		}
		types := make([]string, len(pol.PolicyTypes))
		for i, d := range pol.PolicyTypes {
			types[i] = string(d)
		}
		out = append(out, row{
			Namespace:            pol.Namespace,
			Name:                 pol.Name,
			PodSelector:          idx.FormatSelector(pol.PodSelector),
			PolicyTypes:          types,
			PolicyTypesDefaulted: pol.PolicyTypesDefaulted,
			IngressRules:         len(pol.Ingress),
			EgressRules:          len(pol.Egress),
		})
	}
	if out == nil {
		out = []row{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) explain(w http.ResponseWriter, r *http.Request) {
	idx, err := s.current(r)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	ex, err := idx.Explain(r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ex)
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	idx, err := s.current(r)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	var body struct {
		From struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"from"`
		To struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"to"`
		FromIP   string `json:"fromIP"`
		ToIP     string `json:"toIP"`
		Protocol string `json:"protocol"`
		Port     int32  `json:"port"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, trReq(r, "JSON inválido", "invalid JSON"))
		return
	}
	res, err := idx.Check(eval.Query{
		From:     eval.PodRef{Namespace: body.From.Namespace, Name: body.From.Name},
		FromIP:   body.FromIP,
		To:       eval.PodRef{Namespace: body.To.Namespace, Name: body.To.Name},
		ToIP:     body.ToIP,
		Protocol: body.Protocol,
		Port:     body.Port,
	})
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) graph(w http.ResponseWriter, r *http.Request) {
	idx, err := s.current(r)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	q := r.URL.Query()
	var port int32
	if p := q.Get("port"); p != "" {
		n, err := parsePort(p)
		if err != nil {
			writeErr(w, http.StatusBadRequest, trReq(r, "porta inválida", "invalid port"))
			return
		}
		port = n
	} else {
		port = 80
	}
	g, err := idx.Graph(q.Get("namespace"), q.Get("pod"), q.Get("protocol"), port)
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.Refresh(ctx); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.meta(w, r)
}

func (s *Server) current(r *http.Request) (eval.Index, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snap == nil {
		return eval.Index{}, errors.New(trReq(r, "snapshot ainda não foi carregado", "snapshot is not loaded yet"))
	}
	idx := eval.NewIndex(*s.snap)
	idx.Lang = requestLang(r)
	return idx, nil
}

func requestLang(r *http.Request) string {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang"))) {
	case "en", "en-us", "en-gb":
		return "en"
	default:
		return ""
	}
}

func trReq(r *http.Request, pt, en string) string {
	if requestLang(r) == "en" {
		return en
	}
	return pt
}

func parsePort(s string) (int32, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, errors.New("invalid port")
	}
	return int32(n), nil
}

func writeQueryErr(w http.ResponseWriter, err error) {
	var qe *eval.QueryError
	if errors.As(err, &qe) {
		code := http.StatusBadRequest
		if qe.NotFound {
			code = http.StatusNotFound
		}
		writeErr(w, code, qe.Msg)
		return
	}
	writeErr(w, http.StatusBadRequest, err.Error())
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
