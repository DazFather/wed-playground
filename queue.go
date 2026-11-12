package main

import (
	"context"
	"sync"
	"time"
	"uuid"
)

type connectReq struct {
	id    string
	done  chan struct{}
	admit chan *Playground
}

func NewReqest(ctx context.Context, waitfor time.Duration, q chan<- *connectReq) *connectReq {
	request := &connectReq{
		id:    uuid.New().String(),
		done:  make(chan struct{}, 1),
		admit: make(chan *Playground, 1),
	}

	ctxx, cancel := context.WithTimeout(ctx, waitfor)
	defer cancel()

	select {
	case q <- request:
	case <-ctxx.Done():
		return nil
	}
	return request
}

func (req *connectReq) Handle(ctx context.Context) (interrupt bool) {
	play, err := NewPlayground(req.id)
	if err != nil {
		req.admit <- nil
		return
	}
	defer play.Close()

	if _, err = play.Init(); err != nil {
		req.admit <- nil
		return
	}

	select {
	case req.admit <- play:
		interrupt = play.Wait(ctx)
	case <-req.done:
	case <-ctx.Done():
		interrupt = true
	}
	return
}

func (req *connectReq) Abort() {
	req.done <- struct{}{}
	close(req.done)
}

// LoadConsumers starts `size` workers (active slots) pulling from the waitlist.
func LoadConsumers(ctx context.Context, consumerSize, waitlistSize int) chan<- *connectReq {
	var (
		requests = make(chan *connectReq, waitlistSize)
		wg       sync.WaitGroup
	)

	if consumerSize <= 0 || waitlistSize <= 0 {
		return nil
	}

	go func() {
		defer close(requests)
		for i := 0; i < consumerSize; i++ {
			wg.Go(func() {
				for {
					select {
					case <-ctx.Done():
						return
					case req, ok := <-requests:
						if !ok {
							return
						}
						if req != nil && req.Handle(ctx) {
							return
						}
					}
				}
			})
		}
		wg.Wait()
	}()

	return requests
}
