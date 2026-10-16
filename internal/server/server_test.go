package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pia/porthole/internal/model"
)

type staticSource struct {
	snap model.Snapshot
}

func (s staticSource) Load(ctx context.Context) (model.Snapshot, error) {
	return s.snap, nil
}

func demo() model.Snapshot {
	p := int32(8080)
	return model.Snapshot{
		Namespaces: []model.Namespace{{Name: "app", Labels: map[string]string{"team": "app"}}},
		Pods: []model.Pod{
			{Namespace: "app", Name: "web", Labels: map[string]string{"app": "web"}, IPs: []string{"10.0.0.1"}, Ports: []model.ContainerPort{{Name: "http", Port: 8080, Protocol: "TCP"}}},
			{Namespace: "app", Name: "api", Labels: map[string]string{"app": "api"}, IPs: []string{"10.0.0.2"}},
		},
		Policies: []model.Policy{{
			Namespace:   "app",
			Name:        "web-in",
			PodSelector: model.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			PolicyTypes: []model.Direction{model.Ingress},
			Ingress: []model.Rule{{
				Peers: []model.Peer{{PodSelector: &model.LabelSelector{MatchLabels: map[string]string{"app": "api"}}}},
				Ports: []model.Port{{Protocol: "TCP", Port: &p}},
			}},
		}},
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s := New(staticSource{snap: demo()}, "test", 0)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestExplainAndCheck(t *testing.T) {
	h := newTestServer(t).Handler()

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pods/app/web", nil)
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("explain status %d body %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"ingressIsolated":true`) {
		t.Fatalf("explain body: %s", res.Body.String())
	}

	body := []byte(`{"from":{"namespace":"app","name":"api"},"to":{"namespace":"app","name":"web"},"protocol":"TCP","port":8080}`)
	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/check", bytes.NewReader(body))
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"allowed":true`) {
		t.Fatalf("allow check: %d %s", res.Code, res.Body.String())
	}

	body = []byte(`{"from":{"namespace":"app","name":"web"},"to":{"namespace":"app","name":"api"},"port":8080}`)
	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/check", bytes.NewReader(body))
	h.ServeHTTP(res, req)
	var verdict struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &verdict); err != nil {
		t.Fatal(err)
	}
	// api is not selected, so its ingress is open. web egress is not isolated.
	if !verdict.Allowed {
		t.Fatalf("web → api should be allowed because api is not isolated: %s", res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/pods/missing/nope", nil)
	h.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("missing pod status %d", res.Code)
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "Porthole") {
		t.Fatalf("index: %d %s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "loadAll") {
		t.Fatalf("app.js: %d %s", res.Code, res.Body.String())
	}
}

func TestEnglishAPI(t *testing.T) {
	h := newTestServer(t).Handler()
	body := []byte(`{"from":{"namespace":"app","name":"api"},"to":{"namespace":"app","name":"web"},"port":8080}`)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/check?lang=en", bytes.NewReader(body))
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "is allowed") {
		t.Fatalf("english check: %d %s", res.Code, res.Body.String())
	}
}

func TestCheckDeny(t *testing.T) {
	h := newTestServer(t).Handler()
	body := []byte(`{"from":{"namespace":"app","name":"api"},"to":{"namespace":"app","name":"web"},"port":9}`)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/check", bytes.NewReader(body))
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"allowed":false`) {
		t.Fatalf("deny check: %d %s", res.Code, res.Body.String())
	}
}
