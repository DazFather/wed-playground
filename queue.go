package main

import (
	"context"
	"errors"
	"sync"
	"time"
	"uuid"

	"github.com/DazFather/Wednesday/pkg/shared"
)

var actives shared.Smap[string, *PlayRequest]

func ConnectPlayRequest(requestID string) (*PlayRequest, error) {
	v, ok := (*sync.Map)(&actives).Load(requestID)
	if !ok {
		return nil, errors.New("Cannot connect to '" + requestID + "' playground")
	}
	val, _ := v.(*PlayRequest)
	return val, nil
}

type PlayRequest struct {
	id        string
	created   chan *Playground
	aborted   chan struct{}
	isaborted bool
	err       error
	*Playground
}

func (r *PlayRequest) Abort() {
	r.isaborted = true
	r.aborted <- struct{}{}
}

func (r *PlayRequest) Close() error {
	close(r.aborted)
	close(r.created)
	actives.Delete(r.id)
	return r.Playground.Close()
}

func NewPlayReqest(ctx context.Context) *PlayRequest {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	req := &PlayRequest{
		id:      uuid.New().String(),
		aborted: make(chan struct{}, 1),
		created: make(chan *Playground),
	}

	if len(requests) >= WAITLIST_SIZE {
		return nil
	}

	select {
	case requests <- req:
	case <-ctx.Done():
		return nil
	}
	return req
}

var requests = make(chan *PlayRequest, WAITLIST_SIZE)

func LoadConsumers(ctx context.Context) func() {
	var (
		ctxx, cancel = context.WithCancel(ctx)
		queue        = make(chan *PlayRequest, MAX_CONCURRENT_PLAYS)
		wg           sync.WaitGroup
	)

	for i := 0; i < MAX_CONCURRENT_PLAYS; i++ {
		wg.Go(func() {
			for req := range requests {
				req.Waitlist(ctxx, queue)
			}
		})
		wg.Go(func() {
			for req := range queue {
				req.Active(ctxx)
			}
		})
	}

	return func() {
		cancel()
		close(requests)
		close(queue)
		wg.Wait()
	}
}

func (req *PlayRequest) Waitlist(ctx context.Context, q chan *PlayRequest) {
	ctx, cancel := context.WithTimeout(ctx, MAX_WAITTIME)
	defer cancel()

	select {
	case q <- req:
	case <-req.aborted:
	case <-ctx.Done():
	}
}

func (req *PlayRequest) Active(ctx context.Context) {
	done := make(chan bool, 1)

	go func() {
		defer close(done)
		if req.Playground, req.err = NewPlayground(req.id); req.err == nil && req.isaborted {
			req.Playground.Close()
		}
		done <- req.err == nil && req.Playground != nil
	}()

	select {
	case success := <-done:
		if !success {
			return
		}
	case <-req.aborted:
	case <-ctx.Done():
		return
	}

	defer req.Close()
	actives.Store(req.id, req)
	req.created <- req.Playground

	max, cancel := context.WithTimeout(ctx, MAX_PLAYTIME)
	defer cancel()

	inactive := time.NewTimer(MAX_INACTIVE_TIME)
	defer inactive.Stop()

	dead := req.liveness(max, LIVENESS_PROBE)

	for {
		select {
		case _, ok := <-req.edited:
			if !ok {
				return
			}
			inactive.Reset(MAX_INACTIVE_TIME)
		case <-dead:
			return
		case <-inactive.C:
			return
		case <-max.Done():
			return
		}
	}
}
