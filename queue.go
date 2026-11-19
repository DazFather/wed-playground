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
	abortOnce sync.Once
	closeOnce sync.Once
	err       error
	*Playground
}

func (r *PlayRequest) Abort() {
	r.abortOnce.Do(func() { close(r.aborted) })
}

func (r *PlayRequest) Close() error {
	r.Abort()
	r.closeOnce.Do(func() { actives.Delete(r.id) })
	if r.Playground == nil {
		return nil
	}
	return r.Playground.Close()
}

func NewPlayReqest(ctx, shutdown context.Context) *PlayRequest {
	req := &PlayRequest{
		id:      uuid.New().String(),
		aborted: make(chan struct{}),
		created: make(chan *Playground),
	}

	if len(requests) >= WAITLIST_SIZE {
		return nil
	}

	select {
	case requests <- req:
		return req
	case <-ctx.Done():
	case <-shutdown.Done():
	}
	return nil
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
			for {
				select {
				case req := <-requests:
					req.Waitlist(ctxx, queue)
				case <-ctxx.Done():
					return
				}
			}
		})
		wg.Go(func() {
			for {
				select {
				case req := <-queue:
					req.Active(ctxx)
				case <-ctxx.Done():
					return
				}
			}
		})
	}

	return func() {
		cancel()
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
	defer req.Close()

	req.Playground, req.err = NewPlayground(req.id)
	if req.err != nil || req.Playground == nil {
		return
	}

	actives.Store(req.id, req)
	select {
	case req.created <- req.Playground:
	case <-req.aborted:
	case <-ctx.Done():
	}

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
		case <-req.aborted:
			return
		case <-dead:
			return
		case <-inactive.C:
			return
		case <-max.Done():
			return
		case <-ctx.Done():
			return
		}
	}
}
