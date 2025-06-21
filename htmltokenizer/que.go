package main

import (
	"fmt"
	"sync"
	"time"
)

type CrawledURL struct {
	URL string
}

type PubSub[T any] struct {
	mu          sync.RWMutex
	subscribers map[string][]chan T
}

func NewPubSub[T any]() *PubSub[T] {
	return &PubSub[T]{
		subscribers: make(map[string][]chan T),
	}
}

func (ps *PubSub[T]) Subscribe(topic string) <-chan T {
	ch := make(chan T, 10) // buffered channel
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.subscribers[topic] = append(ps.subscribers[topic], ch)
	return ch
}

func (ps *PubSub[T]) Publish(topic string, msg T) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	for _, ch := range ps.subscribers[topic] {
		ch <- msg
	}
}

func main() {
	ps := NewPubSub[CrawledURL]()

	sub := ps.Subscribe("nexford")

	go func() {
		for msg := range sub {
			fmt.Println("Received:", msg.URL)
		}
	}()

	ps.Publish("nexford", CrawledURL{URL: "https://www.nexford.edu"})
	time.Sleep(1 * time.Millisecond)
}
