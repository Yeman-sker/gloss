// Package openai streams chat completions from any service that speaks the
// OpenAI API. It is not an action: actions such as those of providers/prompt
// call it through the host, which holds the key. It uses net/http alone: the
// streaming format is small, and an SDK would cost more than it saves. Errors
// are shown to the user as they are, so they are in the language of the panel.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	defaultIdle = 60 * time.Second
	maxLine     = 1 << 20
	maxErrBody  = 1 << 16
)

var errStalled = errors.New("模型服务长时间没有响应")

// ErrUnreachable is a service that could not be reached at all: a wrong
// address, no network, or a proxy in the way.
var ErrUnreachable = errors.New("无法连接模型服务")

// StatusError is a service that answered with an HTTP error.
type StatusError struct {
	Code int
	// Message is the service's own, with any key taken out.
	Message string
}

func (e *StatusError) Error() string {
	var why string
	switch {
	case e.Code == 401 || e.Code == 403:
		why = "Key 无效或没有权限"
	case e.Code == 404:
		why = "接口地址或模型名不对"
	case e.Code == 429:
		why = "请求太频繁，或额度已用完"
	case e.Code >= 500:
		why = "模型服务暂时出错"
	default:
		why = "模型服务拒绝了请求"
	}
	msg := fmt.Sprintf("%s（HTTP %d）", why, e.Code)
	if e.Message != "" {
		msg += "：" + e.Message
	}
	return msg
}

// Client is one model of one service.
type Client struct {
	BaseURL string // up to and including the version, like https://api.openai.com/v1
	APIKey  string
	Model   string
	Idle    time.Duration // gives up after this long without data; zero means 60s
}

// Message is one turn of a chat.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Stream sends the messages and calls delta with each piece of the reply as
// it comes. It returns nil only for a reply that finished: one cut short,
// truncated or filtered is an error, whatever came before it.
func (a Client) Stream(ctx context.Context, msgs []Message, delta func(string)) error {
	if a.APIKey == "" {
		return errors.New("缺少 API Key")
	}
	if a.Model == "" {
		return errors.New("缺少模型名称")
	}
	body, err := json.Marshal(chatRequest{Model: a.Model, Stream: true, Messages: msgs})
	if err != nil {
		return err
	}

	// One timer covers both the wait for the first byte and a stream that
	// stops halfway. A timeout on the client would cut off long answers.
	idle := a.Idle
	if idle <= 0 {
		idle = defaultIdle
	}
	runCtx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	timer := time.AfterFunc(idle, func() { stop(errStalled) })
	defer timer.Stop()

	url := strings.TrimRight(a.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(runCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("模型服务地址无效：%w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return failure(runCtx, fmt.Errorf("%w：%v", ErrUnreachable, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return failure(runCtx, statusError(resp, a.APIKey))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4096), maxLine)
	emitted, finished := false, false
	for scanner.Scan() {
		timer.Reset(idle)
		// Other lines are SSE comments and fields that servers use to keep
		// the connection alive.
		data, ok := strings.CutPrefix(scanner.Text(), "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			finished = true
			break
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("模型服务返回了无法解析的数据：%w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("模型服务出错：%s", redact(chunk.Error.Message, a.APIKey))
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				delta(choice.Delta.Content)
				emitted = true
			}
			switch choice.FinishReason {
			case "":
			case "length":
				return errors.New("结果过长，已被模型服务截断")
			case "content_filter":
				return errors.New("内容被模型服务的安全策略拦截")
			default:
				finished = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return failure(runCtx, fmt.Errorf("读取结果失败：%w", err))
	}
	if !emitted {
		return failure(runCtx, errors.New("模型服务没有返回内容"))
	}
	if !finished {
		return failure(runCtx, errors.New("结果不完整，连接提前结束"))
	}
	return nil
}

// failure prefers why the run was stopped over how that looked to net/http.
func failure(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return err
}

func statusError(resp *http.Response, key string) error {
	var body struct {
		Error *apiError `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
	e := &StatusError{Code: resp.StatusCode}
	if json.Unmarshal(raw, &body) == nil && body.Error != nil && body.Error.Message != "" {
		e.Message = redact(body.Error.Message, key)
	}
	return e
}

// keyLike is what services echo of a key, masked or not, as OpenAI's 401
// does ("sk-abcd****wxyz"): even a part of a key stays out of errors.
var keyLike = regexp.MustCompile(`\b(sk|pk|rk)-[A-Za-z0-9*._-]{4,}|[A-Za-z0-9_-]*\*{3,}[A-Za-z0-9_-]*`)

// redact keeps the key out of a service's message: the key itself, in any
// format, and what looks like a part of one.
func redact(msg, key string) string {
	if key = strings.TrimSpace(key); key != "" {
		msg = strings.ReplaceAll(msg, key, "[已隐去]")
	}
	return keyLike.ReplaceAllString(msg, "[已隐去]")
}

type chatRequest struct {
	Model    string    `json:"model"`
	Stream   bool      `json:"stream"`
	Messages []Message `json:"messages"`
}

type apiError struct {
	Message string `json:"message"`
}

type chatChunk struct {
	Error   *apiError `json:"error"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}
