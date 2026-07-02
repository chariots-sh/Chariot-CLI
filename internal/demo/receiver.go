// Package demo implements the local webhook receiver behind `chariot demo serve`:
// a tiny HTTP server that accepts the POSTs Chariot makes to a deploy's
// --endpoint and prints each agent reply to the terminal.
package demo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Reply is the JSON body Chariot POSTs to the customer endpoint
// (OutboundPayload on the backend).
type Reply struct {
	AgentID string  `json:"agent_id"`
	Message string  `json:"message"`
	ReplyTo *string `json:"reply_to"`
}

// Handler accepts webhook deliveries on any path and writes a readable line
// per reply to out. now is injectable for tests; pass time.Now.
func Handler(out io.Writer, now func() time.Time) http.Handler {
	var mu sync.Mutex // serialize prints across concurrent deliveries
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprintln(w, "chariot demo serve — POST agent replies here")
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "reading body", http.StatusBadRequest)
			return
		}

		mu.Lock()
		printDelivery(out, now(), r, body)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
}

func printDelivery(out io.Writer, at time.Time, r *http.Request, body []byte) {
	var reply Reply
	if err := json.Unmarshal(body, &reply); err != nil || reply.Message == "" {
		// Not the shape we expect — show it raw rather than dropping it.
		fmt.Fprintf(out, "[%s] POST %s (unrecognized body)\n  %s\n\n", at.Format("15:04:05"), r.URL.Path, body)
		return
	}
	PrintReply(out, at, reply.AgentID, r.Header.Get("X-Chariot-Account"), reply.ReplyTo, reply.Message)
}

// PrintReply writes one agent reply in the shared demo format, used by both
// the webhook receiver and `demo watch`. account and replyTo may be empty/nil.
func PrintReply(out io.Writer, at time.Time, agentID, account string, replyTo *string, message string) {
	fmt.Fprintf(out, "[%s] agent %s", at.Format("15:04:05"), agentID)
	if account != "" {
		fmt.Fprintf(out, " · account %s", account)
	}
	if replyTo != nil && *replyTo != "" {
		fmt.Fprintf(out, " · reply-to %s", *replyTo)
	}
	fmt.Fprintf(out, "\n  %s\n\n", message)
}
