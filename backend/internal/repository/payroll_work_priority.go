package repository

import (
	"context"
	"errors"
	"sync"
	"time"
)

var errPayrollWorkYielded = errors.New("payroll preparation yielded to an interactive edit")

// PayrollWorkPriority is a scheduling hint shared by the API's board writer and
// derived worker. It never replaces database locks or durable job leases. Only
// disposable background work is canceled; financial commands are not registered.
type PayrollWorkPriority struct {
	mu          sync.Mutex
	foreground  int
	quietUntil  time.Time
	changed     chan struct{}
	next        uint64
	backgrounds map[uint64]context.CancelCauseFunc
}

func NewPayrollWorkPriority() *PayrollWorkPriority {
	return &PayrollWorkPriority{changed: make(chan struct{}), backgrounds: map[uint64]context.CancelCauseFunc{}}
}

func (r *GrossBoardRepository) ConfigurePayrollWorkPriority(priority *PayrollWorkPriority) {
	r.workPriority = priority
}
func (r *DriverPayRepository) ConfigurePayrollWorkPriority(priority *PayrollWorkPriority) {
	r.workPriority = priority
}

func (p *PayrollWorkPriority) interactive() func() {
	if p == nil {
		return func() {}
	}
	p.mu.Lock()
	p.foreground++
	for _, cancel := range p.backgrounds {
		cancel(errPayrollWorkYielded)
	}
	close(p.changed)
	p.changed = make(chan struct{})
	p.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.foreground--
			if p.foreground == 0 {
				p.quietUntil = time.Now().Add(500 * time.Millisecond)
			}
			close(p.changed)
			p.changed = make(chan struct{})
		})
	}
}

func (p *PayrollWorkPriority) background(ctx context.Context) (context.Context, func(), error) {
	if p == nil {
		return ctx, func() {}, ctx.Err()
	}
	for {
		p.mu.Lock()
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return ctx, func() {}, err
		}
		remaining := time.Until(p.quietUntil)
		if p.foreground == 0 && remaining <= 0 {
			work, cancel := context.WithCancelCause(ctx)
			p.next++
			id := p.next
			p.backgrounds[id] = cancel
			p.mu.Unlock()
			return work, func() { p.mu.Lock(); delete(p.backgrounds, id); p.mu.Unlock(); cancel(context.Canceled) }, nil
		}
		changed := p.changed
		var timer *time.Timer
		var elapsed <-chan time.Time
		if p.foreground == 0 {
			timer = time.NewTimer(remaining)
			elapsed = timer.C
		}
		p.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		case <-elapsed:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}
