package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ali-aliabadi/relay/internal/obs"
)

// DefaultAPIURL is the real Bot API. Tests and e2e point the client elsewhere.
const DefaultAPIURL = "https://api.telegram.org"

// Client calls the Telegram Bot API. The token is part of every URL, so
// errors from net/http are stripped of their URL before being returned.
type Client struct {
	baseURL string
	token   obs.Secret
	http    *http.Client
}

// NewClient returns a client for baseURL (DefaultAPIURL in production).
func NewClient(baseURL string, token obs.Secret, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: hc}
}

// apiError is a Bot API error response. Description comes from Telegram and
// describes the request's problem (not its content).
type apiError struct {
	Status      int
	Code        int
	Description string
	RetryAfter  time.Duration
}

func (e *apiError) Error() string {
	return fmt.Sprintf("telegram api error %d: %s", e.Code, e.Description)
}

type response struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// call posts a JSON body to a Bot API method and decodes result into out.
func (c *Client) call(ctx context.Context, method string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", method, err)
	}
	return c.do(ctx, method, "application/json", bytes.NewReader(payload), out)
}

// callMultipart posts form fields plus one file upload.
func (c *Client) callMultipart(ctx context.Context, method string, fields map[string]string, fileField string, file []byte, contentType string, out any) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return fmt.Errorf("encoding %s: %w", method, err)
		}
	}
	ext := ".png"
	if contentType == "image/jpeg" {
		ext = ".jpg"
	}
	fw, err := mw.CreateFormFile(fileField, "image"+ext)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", method, err)
	}
	if _, err := fw.Write(file); err != nil {
		return fmt.Errorf("encoding %s: %w", method, err)
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("encoding %s: %w", method, err)
	}
	return c.do(ctx, method, mw.FormDataContentType(), &buf, out)
}

func (c *Client) do(ctx context.Context, method, contentType string, body io.Reader, out any) error {
	endpoint := c.baseURL + "/bot" + c.token.Reveal() + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("building %s request: %w", method, scrub(err))
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s: %w", method, scrub(err))
	}
	defer resp.Body.Close()
	var r response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return &apiError{Status: resp.StatusCode, Code: resp.StatusCode, Description: "unreadable response"}
	}
	if !r.OK {
		return &apiError{
			Status: resp.StatusCode, Code: r.ErrorCode, Description: truncate(r.Description, 200),
			RetryAfter: time.Duration(r.Parameters.RetryAfter) * time.Second,
		}
	}
	if out != nil {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("decoding %s result: %w", method, err)
		}
	}
	return nil
}

// scrub drops the URL (which holds the bot token) from net/http errors.
func scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s request: %w", ue.Op, ue.Err)
	}
	return err
}

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
