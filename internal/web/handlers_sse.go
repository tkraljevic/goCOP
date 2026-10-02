package web

import (
	"fmt"
	"net/http"
	"time"

	"gocop/internal/service"
)

type SSEHandler struct {
	broker *service.SSEBroker
}

// razmakPinga je koliko tok događaja smije šutjeti. Cloudflare prekida
// vezu bez prometa nakon 100 s, a i posrednici u lokalnoj mreži znaju
// zatvoriti tihu vezu; komentar ": ping" preglednik preskače.
var razmakPinga = 25 * time.Second

func NewSSEHandler(broker *service.SSEBroker) *SSEHandler {
	return &SSEHandler{broker: broker}
}

// ServeSSE pruža Server-Sent Events stream za trenutnu sinkronizaciju svih online klijenata
func (h *SSEHandler) ServeSSE(w http.ResponseWriter, r *http.Request) {
	// ResponseController nađe Flush i kroz omotače odgovora (zaštitni slojevi)
	rc := http.NewResponseController(w)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientChan := h.broker.Subscribe()
	defer h.broker.Unsubscribe(clientChan)

	// Početni handshake
	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"online\"}\n\n")
	if err := rc.Flush(); err != nil {
		return // odgovor se ne da slati u dijelovima
	}

	ping := time.NewTicker(razmakPinga)
	defer ping.Stop()
	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			_ = rc.Flush()
		case msg, ok := <-clientChan:
			if !ok {
				return
			}
			fmt.Fprint(w, msg)
			_ = rc.Flush()
		}
	}
}
