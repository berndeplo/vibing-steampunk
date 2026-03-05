package adt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRfcTransport_BasicGet(t *testing.T) {
	var receivedReq ProxyRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedReq)

		resp := ProxyResponse{
			StatusCode:   200,
			ReasonPhrase: "OK",
			Headers:      map[string]string{"X-Custom": "value"},
			Body:         "<programs/>",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := NewConfig("http://sap:50000", "user", "pass", WithClient("001"), WithLanguage("EN"))
	transport := NewRfcTransport(server.URL, cfg, 5)

	resp, err := transport.Request(context.Background(), "/sap/bc/adt/programs/programs/ZTEST", nil)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Verify proxy request sent to sidecar
	if receivedReq.Method != "GET" {
		t.Errorf("Method = %v, want GET", receivedReq.Method)
	}
	if receivedReq.URI == "" {
		t.Fatal("URI should not be empty")
	}

	// URI should contain path and query params
	u, _ := url.Parse(receivedReq.URI)
	if u.Path != "/sap/bc/adt/programs/programs/ZTEST" {
		t.Errorf("URI path = %v, want /sap/bc/adt/programs/programs/ZTEST", u.Path)
	}
	if u.Query().Get("sap-client") != "001" {
		t.Errorf("sap-client = %v, want 001", u.Query().Get("sap-client"))
	}
	if u.Query().Get("sap-language") != "EN" {
		t.Errorf("sap-language = %v, want EN", u.Query().Get("sap-language"))
	}

	// Verify response
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %v, want 200", resp.StatusCode)
	}
	if string(resp.Body) != "<programs/>" {
		t.Errorf("Body = %v, want <programs/>", string(resp.Body))
	}
	if resp.Headers.Get("X-Custom") != "value" {
		t.Errorf("X-Custom header = %v, want value", resp.Headers.Get("X-Custom"))
	}
}

func TestRfcTransport_PostWithBody(t *testing.T) {
	var receivedReq ProxyRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedReq)

		resp := ProxyResponse{StatusCode: 200, ReasonPhrase: "OK"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := NewConfig("http://sap:50000", "user", "pass")
	transport := NewRfcTransport(server.URL, cfg, 5)

	_, err := transport.Request(context.Background(), "/sap/bc/adt/programs/programs/ZTEST", &RequestOptions{
		Method:      http.MethodPost,
		Body:        []byte("<source>REPORT ZTEST.</source>"),
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if receivedReq.Method != "POST" {
		t.Errorf("Method = %v, want POST", receivedReq.Method)
	}
	if receivedReq.Body != "<source>REPORT ZTEST.</source>" {
		t.Errorf("Body = %v, want <source>REPORT ZTEST.</source>", receivedReq.Body)
	}
	if receivedReq.Headers["Content-Type"] != "text/plain" {
		t.Errorf("Content-Type = %v, want text/plain", receivedReq.Headers["Content-Type"])
	}
}

func TestRfcTransport_SessionCookies(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		var req ProxyRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)

		resp := ProxyResponse{
			StatusCode:   200,
			ReasonPhrase: "OK",
		}

		if callCount == 1 {
			// First call: return a session cookie
			resp.Headers = map[string]string{
				"Set-Cookie": "sap-contextid=ctx-abc-123; path=/",
			}
		} else {
			// Second call: verify session cookie was forwarded
			cookie := req.Headers["Cookie"]
			if cookie != "sap-contextid=ctx-abc-123" {
				t.Errorf("Cookie = %v, want sap-contextid=ctx-abc-123", cookie)
			}
		}

		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := NewConfig("http://sap:50000", "user", "pass")
	transport := NewRfcTransport(server.URL, cfg, 5)

	// First request — should receive session cookie
	_, err := transport.Request(context.Background(), "/sap/bc/adt/test", nil)
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}

	// Second request — should forward session cookie
	_, err = transport.Request(context.Background(), "/sap/bc/adt/test", nil)
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}

	if callCount != 2 {
		t.Errorf("Expected 2 calls, got %d", callCount)
	}
}

func TestRfcTransport_ErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"404 Not Found", 404, "Object not found"},
		{"500 Internal Server Error", 500, "Internal server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := ProxyResponse{
					StatusCode:   tt.statusCode,
					ReasonPhrase: tt.name,
					Body:         tt.body,
				}
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			cfg := NewConfig("http://sap:50000", "user", "pass")
			transport := NewRfcTransport(server.URL, cfg, 5)

			_, err := transport.Request(context.Background(), "/sap/bc/adt/test", nil)
			if err == nil {
				t.Fatal("Expected error for error status code")
			}

			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("Expected *APIError, got %T: %v", err, err)
			}
			if apiErr.StatusCode != tt.statusCode {
				t.Errorf("StatusCode = %v, want %v", apiErr.StatusCode, tt.statusCode)
			}
			if apiErr.Message != tt.body {
				t.Errorf("Message = %v, want %v", apiErr.Message, tt.body)
			}
		})
	}
}

func TestRfcTransport_SidecarDown(t *testing.T) {
	cfg := NewConfig("http://sap:50000", "user", "pass")
	// Point to a port where nothing is listening
	transport := NewRfcTransport("http://localhost:19999", cfg, 5)
	transport.httpClient.Timeout = 1 * time.Second

	_, err := transport.Request(context.Background(), "/sap/bc/adt/test", nil)
	if err == nil {
		t.Fatal("Expected error when sidecar is down")
	}

	// Should mention the sidecar URL in the error
	errMsg := err.Error()
	if !strings.Contains(errMsg, "sidecar") {
		t.Errorf("Error should mention sidecar, got: %v", errMsg)
	}
}

func TestRfcTransport_ConcurrencyLimit(t *testing.T) {
	maxConcurrent := 2
	var active int64
	var maxActive int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt64(&active, 1)
		defer atomic.AddInt64(&active, -1)

		// Track max concurrency
		for {
			old := atomic.LoadInt64(&maxActive)
			if current <= old || atomic.CompareAndSwapInt64(&maxActive, old, current) {
				break
			}
		}

		// Hold the request briefly to allow concurrency to build
		time.Sleep(50 * time.Millisecond)

		resp := ProxyResponse{StatusCode: 200, ReasonPhrase: "OK"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := NewConfig("http://sap:50000", "user", "pass")
	transport := NewRfcTransport(server.URL, cfg, maxConcurrent)

	// Fire 6 requests in parallel
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			transport.Request(context.Background(), "/sap/bc/adt/test", nil)
		}()
	}
	wg.Wait()

	observed := atomic.LoadInt64(&maxActive)
	if observed > int64(maxConcurrent) {
		t.Errorf("Max concurrent requests = %d, want <= %d", observed, maxConcurrent)
	}
}

func TestRfcTransport_QueryParams(t *testing.T) {
	var receivedReq ProxyRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedReq)

		resp := ProxyResponse{StatusCode: 200, ReasonPhrase: "OK"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := NewConfig("http://sap:50000", "user", "pass", WithClient("100"), WithLanguage("DE"))
	transport := NewRfcTransport(server.URL, cfg, 5)

	query := url.Values{}
	query.Set("custom", "value")

	_, err := transport.Request(context.Background(), "/sap/bc/adt/test", &RequestOptions{
		Query: query,
	})
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	u, _ := url.Parse(receivedReq.URI)
	q := u.Query()
	if q.Get("sap-client") != "100" {
		t.Errorf("sap-client = %v, want 100", q.Get("sap-client"))
	}
	if q.Get("sap-language") != "DE" {
		t.Errorf("sap-language = %v, want DE", q.Get("sap-language"))
	}
	if q.Get("custom") != "value" {
		t.Errorf("custom = %v, want value", q.Get("custom"))
	}
}

func TestRfcTransport_StatefulSession(t *testing.T) {
	var receivedReq ProxyRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedReq)

		resp := ProxyResponse{StatusCode: 200, ReasonPhrase: "OK"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := NewConfig("http://sap:50000", "user", "pass", WithSessionType(SessionStateful))
	transport := NewRfcTransport(server.URL, cfg, 5)

	_, err := transport.Request(context.Background(), "/sap/bc/adt/test", nil)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if receivedReq.Headers["X-sap-adt-sessiontype"] != "stateful" {
		t.Errorf("X-sap-adt-sessiontype = %v, want stateful", receivedReq.Headers["X-sap-adt-sessiontype"])
	}
}

func TestRfcTransport_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // Simulate slow sidecar
		resp := ProxyResponse{StatusCode: 200, ReasonPhrase: "OK"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := NewConfig("http://sap:50000", "user", "pass")
	transport := NewRfcTransport(server.URL, cfg, 5)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := transport.Request(ctx, "/sap/bc/adt/test", nil)
	if err == nil {
		t.Fatal("Expected error on cancelled context")
	}
}

func TestExtractContextID(t *testing.T) {
	tests := []struct {
		name   string
		cookie string
		want   string
	}{
		{"simple", "sap-contextid=abc123; path=/", "abc123"},
		{"no match", "other=value; path=/", ""},
		{"empty", "", ""},
		{"multiple cookies comma-separated", "session=x, sap-contextid=def456; path=/", "def456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractContextID(tt.cookie)
			if got != tt.want {
				t.Errorf("extractContextID(%q) = %v, want %v", tt.cookie, got, tt.want)
			}
		})
	}
}

