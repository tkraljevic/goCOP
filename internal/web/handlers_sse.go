package web

import (
	"fmt"
	"net/http"

	"gocop/internal/service"
)

type SSEHandler struct {
	broker *service.SSEBroker
}

func NewSSEHandler(broker *service.SSEBroker) *SSEHandler {
	return &SSEHandler{broker: broker}
}

// ServeSSE pruža Server-Sent Events stream za trenutnu sinkronizaciju svih online klijenata
func (h *SSEHandler) ServeSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming nije podržan", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	clientChan := h.broker.Subscribe()
	defer h.broker.Unsubscribe(clientChan)

	// Početni handshake
	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"online\"}\n\n")
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg, ok := <-clientChan:
			if !ok {
				return
			}
			fmt.Fprint(w, msg)
			flusher.Flush()
		}
	}
}
