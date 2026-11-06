package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testKey = "test-key"

func delta(text string) string {
	b, _ := json.Marshal(text)
	return fmt.Sprintf(`data: {"choices":[{"delta":{"content":%s},"finish_reason":null}]}`, b)
}

// serve answers every request with lines, flushed one by one.
func serve(t *testing.T, status int, lines ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		for _, line := range lines {
			fmt.Fprintf(w, "%s\n\n", line)
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

var hello = []Message{{"user", "hello"}}

func run(a Client) (string, error) {
	var out strings.Builder
	err := a.Stream(context.Background(), hello, func(d string) { out.WriteString(d) })
	return out.String(), err
}

func TestStream(t *testing.T) {
	var got chatRequest
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		for _, line := range []string{
			": keep-alive",
			`data: {"choices":[]}`,
			delta("你"),
			"data:" + strings.TrimPrefix(delta("好"), "data: "),
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			"data: [DONE]",
		} {
			fmt.Fprintf(w, "%s\n\n", line)
		}
	}))
	defer srv.Close()

	var deltas []string
	msgs := []Message{{"system", "be brief"}, {"user", "hello"}}
	err := Client{BaseURL: srv.URL + "/v1/", APIKey: testKey, Model: "m"}.Stream(context.Background(), msgs,
		func(d string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(deltas, "|") != "你|好" {
		t.Errorf("deltas = %q", deltas)
	}
	if path != "/v1/chat/completions" {
		t.Errorf("path = %s", path)
	}
	if auth != "Bearer "+testKey {
		t.Errorf("authorization = %s", auth)
	}
	if !got.Stream || got.Model != "m" || len(got.Messages) != 2 || got.Messages[1].Content != "hello" {
		t.Errorf("request = %+v", got)
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content != "be brief" {
		t.Errorf("system message = %+v", got.Messages[0])
	}
}

func TestEndWithoutDoneMarker(t *testing.T) {
	srv := serve(t, 200, delta("好"), `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`)
	if out, err := run(Client{BaseURL: srv.URL, APIKey: testKey, Model: "m"}); err != nil || out != "好" {
		t.Errorf("out = %q, err = %v", out, err)
	}
}

func TestCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s\n\n", delta("你"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Client{BaseURL: srv.URL, APIKey: testKey, Model: "m"}.Stream(ctx, hello,
			func(string) { cancel() })
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestCancelBeforeResponse(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := Client{BaseURL: srv.URL, APIKey: testKey, Model: "m"}.Stream(ctx, hello,
		func(string) {})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
}

func TestStalled(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s\n\n", delta("你"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	out, err := run(Client{BaseURL: srv.URL, APIKey: testKey, Model: "m", Idle: 50 * time.Millisecond})
	if !errors.Is(err, errStalled) || out != "你" {
		t.Errorf("out = %q, err = %v", out, err)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		lines  []string
		want   string
		out    string
	}{
		{name: "api message", status: 401, lines: []string{`{"error":{"message":"bad key"}}`}, want: "HTTP 401：bad key"},
		{name: "html body", status: 502, lines: []string{"<html>Bad Gateway</html>"}, want: "HTTP 502"},
		{name: "error in stream", status: 200, lines: []string{delta("你"), `data: {"error":{"message":"overloaded"}}`}, want: "overloaded", out: "你"},
		{name: "bad json", status: 200, lines: []string{"data: {"}, want: "无法解析"},
		{name: "no content", status: 200, lines: []string{"data: [DONE]"}, want: "没有返回内容"},
		{name: "not a stream", status: 200, lines: []string{`{"choices":[]}`}, want: "没有返回内容"},
		{name: "cut short", status: 200, lines: []string{delta("你")}, want: "连接提前结束", out: "你"},
		{name: "length", status: 200, lines: []string{delta("你"), `data: {"choices":[{"delta":{},"finish_reason":"length"}]}`}, want: "截断", out: "你"},
		{name: "filtered", status: 200, lines: []string{`data: {"choices":[{"delta":{},"finish_reason":"content_filter"}]}`}, want: "拦截"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := serve(t, c.status, c.lines...)
			out, err := run(Client{BaseURL: srv.URL, APIKey: testKey, Model: "m"})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to contain %q", err, c.want)
			}
			if out != c.out {
				t.Errorf("out = %q, want %q", out, c.out)
			}
		})
	}
}

func TestMissingConfig(t *testing.T) {
	if _, err := run(Client{BaseURL: "http://127.0.0.1:0", Model: "m"}); err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Errorf("no key: %v", err)
	}
	if _, err := run(Client{BaseURL: "http://127.0.0.1:0", APIKey: testKey}); err == nil || !strings.Contains(err.Error(), "模型") {
		t.Errorf("no model: %v", err)
	}
}

func TestConnectionFailureHidesKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := run(Client{BaseURL: url, APIKey: testKey, Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "无法连接") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("error leaks the key: %v", err)
	}
}

func TestErrorsHideEchoedKeys(t *testing.T) {
	got := redact("Incorrect API key provided: sk-inval****moke. You can find your API key at https://platform.openai.com/account/api-keys.", "")
	if strings.Contains(got, "inval") || strings.Contains(got, "moke") || !strings.Contains(got, "Incorrect API key provided") {
		t.Fatalf("%q", got)
	}
}

// A service may echo the whole key, in its own format, in a status error
// or inside the stream.
func TestServiceErrorsHideTheKey(t *testing.T) {
	const key = "tok_9f8e7d6c5b4a39281706"
	for name, h := range map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, `{"error":{"message":"key %s is not allowed"}}`, key)
		},
		"stream": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"error\":{\"message\":\"key %s is not allowed\"}}\n\n", key)
		},
	} {
		srv := httptest.NewServer(h)
		_, err := run(Client{BaseURL: srv.URL, APIKey: key, Model: "m"})
		srv.Close()
		if err == nil || strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
