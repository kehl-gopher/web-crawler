package main

import (
	"fmt"
	"sync"
)

type PubSub[T any] struct {
	pubLock     sync.RWMutex
	subscribers map[string][]chan T
	wg          *sync.WaitGroup
	closed      bool
}

func newPubsub[T any]() *PubSub[T] {
	return &PubSub[T]{
		subscribers: make(map[string][]chan T),
		wg:          new(sync.WaitGroup),
	}
}
func (p *PubSub[T]) publish(topic string, message T) {
	p.pubLock.RLock()

	if p.closed {
		fmt.Println("closing!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		return
	}
	p.pubLock.RUnlock()
	if chans, ok := p.subscribers[topic]; ok {
		for _, ch := range chans {
			ch <- message
		}
	}
}

func (ps *PubSub[T]) subscribe(topic string, buffer int) <-chan T {
	ps.pubLock.Lock()
	defer ps.pubLock.Unlock()

	ch := make(chan T, buffer)
	ps.subscribers[topic] = append(ps.subscribers[topic], ch)
	return ch
}

func (ps *PubSub[T]) Unsubscribe(topic string, target <-chan T) {
	ps.pubLock.Lock()
	defer ps.pubLock.Unlock()

	chans := ps.subscribers[topic]
	for i, ch := range chans {
		if ch == target {
			close(ch)
			ps.subscribers[topic] = append(chans[:i], chans[i+1:]...)
			ps.wg.Done()
			break
		}
	}
}

func (ps *PubSub[T]) Shutdown() {
	ps.pubLock.Lock()
	ps.closed = true
	for _, subs := range ps.subscribers {
		for _, ch := range subs {
			close(ch)
		}
	}
	ps.pubLock.Unlock()

	ps.wg.Wait()
}
