// Package alert pushes one-line operator alerts (D27). It speaks the
// ntfy.sh convention: a plain-text POST to a topic URL.
package alert

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Notifier interface {
	Notify(ctx context.Context, msg string) error
}

// Nop is used when ALERT_WEBHOOK_URL is unset; the logs remain the record.
type Nop struct{}

func (Nop) Notify(context.Context, string) error { return nil }

type Webhook struct {
	url    string
	client *http.Client
}

func NewWebhook(url string, client *http.Client) *Webhook {
	return &Webhook{url: url, client: client}
}

func (w *Webhook) Notify(ctx context.Context, msg string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, strings.NewReader(msg))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("alert webhook: status %d", resp.StatusCode)
	}
	return nil
}
