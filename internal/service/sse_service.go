package service

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// SSEEvent predstavlja strukturu poruke poslane prema klijentima
type SSEEvent struct {
	Type      string `json:"type"`    // npr. "users_updated", "assignment_added", "system_alert"
	Message   string `json:"message"` // opis za prikaz u obavijestima
	Payload   any    `json:"payload"` // podaci promjene
	Timestamp string `json:"timestamp"`
}

// SSEBroker upravlja pretplatama i emitiranjem u stvarnom vremenu
type SSEBroker struct {
	mu      sync.RWMutex
	clients map[chan string]bool
}

func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[chan string]bool),
	}
}

// Subscribe registrira novog povezanog klijenta (browser ili vanjski čvor)
func (b *SSEBroker) Subscribe() chan string {
	ch := make(chan string, 16)
	b.mu.Lock()
	b.clients[ch] = true
	b.mu.Unlock()
	return ch
}

// Unsubscribe odjavljuje klijenta
func (b *SSEBroker) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	close(ch)
	b.mu.Unlock()
}

// Broadcast šalje događaj svim povezanim klijentima u djeliću sekunde
func (b *SSEBroker) Broadcast(eventType string, message string, payload any) {
	event := SSEEvent{
		Type:      eventType,
		Message:   message,
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	rawMsg := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(data))

	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.clients {
		select {
		case ch <- rawMsg:
		default:
			// Ako klijent ne stiže primati, preskačemo kako ne bi blokirali ostale
		}
	}
}
