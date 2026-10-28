package handlers

import (
	"errors"
	"sync"
)

var (
	ErrTooManySubscribers = errors.New("limite de conexões em tempo real atingido")
	ErrBrokerClosed       = errors.New("serviço de eventos encerrado")
)

// Broker avisa as conexões abertas (SSE) de que os dados mudaram.
// Cada inscrito tem um canal de capacidade 1: avisos repetidos são fundidos em um só,
// então Publish nunca bloqueia, mesmo com um navegador lento ou travado.
type Broker struct {
	mu     sync.Mutex
	subs   map[chan struct{}]struct{}
	max    int
	done   chan struct{}
	closed bool
}

// NewBroker cria um broker que aceita no máximo max inscritos simultâneos
// (protege a memória do Raspberry Pi).
func NewBroker(max int) *Broker {
	return &Broker{
		subs: make(map[chan struct{}]struct{}),
		max:  max,
		done: make(chan struct{}),
	}
}

// Subscribe registra um inscrito. A função devolvida cancela a inscrição e pode ser chamada mais de uma vez.
func (b *Broker) Subscribe() (<-chan struct{}, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, nil, ErrBrokerClosed
	}
	if len(b.subs) >= b.max {
		return nil, nil, ErrTooManySubscribers
	}

	ch := make(chan struct{}, 1)
	b.subs[ch] = struct{}{}

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs, ch)
	}
	return ch, unsubscribe, nil
}

// Publish avisa todos os inscritos de que algo mudou, sem bloquear.
func (b *Broker) Publish() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default: // já há um aviso pendente
		}
	}
}

// Len devolve quantos inscritos estão conectados.
func (b *Broker) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// Done é fechado quando o broker é encerrado (shutdown do servidor).
func (b *Broker) Done() <-chan struct{} { return b.done }

// Close encerra o broker; as conexões abertas observam Done e terminam.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.done)
}
