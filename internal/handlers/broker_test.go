package handlers

import (
	"errors"
	"testing"
	"time"
)

func received(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-time.After(200 * time.Millisecond):
		return false
	}
}

func pending(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestBroker_PublishNotifiesAllSubscribers(t *testing.T) {
	b := NewBroker(10)
	a, unsubA, _ := b.Subscribe()
	c, unsubC, _ := b.Subscribe()
	defer unsubA()
	defer unsubC()

	b.Publish()

	if !received(a) || !received(c) {
		t.Error("todos os inscritos deveriam ser avisados")
	}
}

func TestBroker_PublishCoalescesAndNeverBlocks(t *testing.T) {
	b := NewBroker(10)
	ch, unsub, _ := b.Subscribe()
	defer unsub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.Publish() // ninguém está lendo: não pode travar
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish travou com inscrito lento")
	}

	if !pending(ch) {
		t.Fatal("deveria haver um aviso pendente")
	}
	if pending(ch) {
		t.Error("avisos repetidos deveriam ser fundidos em um só")
	}
}

func TestBroker_UnsubscribeStopsDelivery(t *testing.T) {
	b := NewBroker(10)
	ch, unsub, _ := b.Subscribe()
	if b.Len() != 1 {
		t.Fatalf("Len = %d, quer 1", b.Len())
	}

	unsub()
	unsub() // idempotente

	if b.Len() != 0 {
		t.Errorf("Len = %d, quer 0", b.Len())
	}
	b.Publish()
	if pending(ch) {
		t.Error("inscrito removido não deve receber avisos")
	}
}

func TestBroker_LimitsSubscribers(t *testing.T) {
	b := NewBroker(2)
	_, unsub1, _ := b.Subscribe()
	_, _, _ = b.Subscribe()

	if _, _, err := b.Subscribe(); !errors.Is(err, ErrTooManySubscribers) {
		t.Fatalf("quer ErrTooManySubscribers, veio %v", err)
	}

	unsub1()
	if _, _, err := b.Subscribe(); err != nil {
		t.Errorf("com vaga liberada deveria aceitar, veio %v", err)
	}
}

func TestBroker_CloseSignalsDone(t *testing.T) {
	b := NewBroker(10)
	b.Close()
	b.Close() // idempotente

	select {
	case <-b.Done():
	default:
		t.Error("Done deveria estar fechado após Close")
	}
	if _, _, err := b.Subscribe(); !errors.Is(err, ErrBrokerClosed) {
		t.Errorf("quer ErrBrokerClosed, veio %v", err)
	}
	b.Publish() // não pode entrar em pânico
}
