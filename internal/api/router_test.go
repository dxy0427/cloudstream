package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHealthz(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	InitRouter().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"status\":\"ok\"}" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestNoRouteOnlyFallsBackForHTMLNavigation(t *testing.T) {
	if err := os.Mkdir("public", 0755); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile("public/index.html", []byte("spa"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll("public") })
	router := InitRouter()
	for _, test := range []struct {
		path       string
		accept     string
		wantStatus int
	}{
		{path: "/missing.js", accept: "*/*", wantStatus: http.StatusNotFound},
		{path: "/api/missing", accept: "text/html", wantStatus: http.StatusNotFound},
		{path: "/apiary", accept: "text/html", wantStatus: http.StatusOK},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("Accept", test.accept)
		router.ServeHTTP(recorder, request)
		if recorder.Code != test.wantStatus {
			t.Fatalf("path=%q status=%d body=%q", test.path, recorder.Code, recorder.Body.String())
		}
	}
}
