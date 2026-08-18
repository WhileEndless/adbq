package adb

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrUnsupported reports that a Strategy cannot work on this device at all —
// the command is missing, was removed, or refuses the caller's privileges.
// A Resolver rules the strategy out for that device and moves on to the next
// one. Return it only for permanent conditions: a transient failure (device
// briefly offline, timeout) must be reported as an ordinary error so the
// strategy stays eligible for later calls.
var ErrUnsupported = errors.New("adb: strategy unsupported on this device")

// ErrNoStrategy reports that no registered Strategy is eligible for the device
// (every one is gated out by SDK level, a missing binary, or root), so the fact
// cannot be read there at all.
var ErrNoStrategy = errors.New("adb: no strategy available for device")

// factTTL bounds how long a Costly strategy's cached value is served when its
// freshness key has not changed. The key is the real invalidation signal; this
// is only a backstop for state that changes without moving the key.
const factTTL = 10 * time.Minute

// Requirements states what a device must provide for a Strategy to run. The
// zero value means "works everywhere", which is what a last-resort strategy
// wants.
type Requirements struct {
	// MinSDK/MaxSDK bound the API levels the strategy applies to; 0 means
	// unbounded. MaxSDK is inclusive and exists for commands that later Android
	// releases removed.
	MinSDK int
	MaxSDK int

	// Bins names the device binaries the strategy needs, using the same names
	// as capBins (see capabilities.go).
	Bins []string

	// Root marks a strategy that only works as root.
	Root bool

	// Costly marks a strategy that is expensive or has side effects on the
	// device, so it must not run on a polling path. A Resolver runs it only
	// when the caller's freshness key changes (or factTTL elapses) and serves
	// the remembered value otherwise.
	Costly bool
}

// Strategy reads one device fact one particular way. Several strategies back a
// single fact so that each Android generation can use the cheapest command it
// offers while older devices keep a working path. Implementations are
// stateless: the Resolver owns all caching.
type Strategy[T any] interface {
	// Name identifies the strategy in cache bookkeeping. It must be stable and
	// unique within a Resolver.
	Name() string

	// Requires reports the device conditions this strategy needs.
	Requires() Requirements

	// Run reads the fact. It returns ErrUnsupported when the device cannot
	// support this path at all.
	Run(ctx context.Context, c *Client, serial string) (T, error)
}

// Resolver picks and runs the best Strategy for a device, and is the only place
// that decides which one applies. Feature code calls a single method (see
// Client.SSID for the shape) and never branches on API level itself.
//
// Registration order is preference order — best (cheapest, most accurate) first.
// Resolve filters the strategies through the device's cached Capabilities and
// tries what is left in that order, so a preferred path is used whenever it
// works. Strategies that report ErrUnsupported are remembered and skipped from
// then on, which is what keeps repeated calls down to a single command.
type Resolver[T any] struct {
	fact       string
	strategies []Strategy[T]
}

// NewResolver registers strategies for a named fact, cheapest-first. The fact
// name keys the per-device cache, so it must be unique across resolvers; use a
// dotted form like "net.ssid". The first segment MUST be a Domain (see
// cachedomain.go): domain invalidation matches on that prefix, so a fact named
// outside the scheme can never be invalidated except by dropping the device.
func NewResolver[T any](fact string, strategies ...Strategy[T]) *Resolver[T] {
	return &Resolver[T]{fact: fact, strategies: strategies}
}

// Resolve returns the fact for serial.
//
// freshKey is cheap, caller-side evidence of the state the fact depends on —
// for a Wi-Fi fact, the interface address and link flags. A Costly strategy
// re-runs only when that key changes, which is what keeps an expensive probe
// off a polling path without going stale: when the device's state moves, the
// key moves with it. An empty key is valid and stable (it means "no such state
// right now"), so a negative result is cached too.
//
// Cheap strategies are not cached; they run on every call and stay maximally
// fresh.
func (r *Resolver[T]) Resolve(ctx context.Context, c *Client, serial, freshKey string) (T, error) {
	var zero T
	if c == nil {
		return zero, fmt.Errorf("%s: %w", r.fact, ErrNoStrategy)
	}

	if v, ok := c.cachedFact(r.fact, serial, freshKey); ok {
		val, _ := v.(T)
		return val, nil
	}

	eligible := r.eligible(ctx, c, serial)
	if len(eligible) == 0 {
		return zero, fmt.Errorf("%s: %w", r.fact, ErrNoStrategy)
	}

	var lastErr error
	for _, s := range eligible {
		val, err := s.Run(ctx, c, serial)
		switch {
		case err == nil:
			if s.Requires().Costly {
				c.rememberFact(r.fact, serial, freshKey, val)
			}
			return val, nil
		case errors.Is(err, ErrUnsupported):
			// Permanent for this device: stop paying for it on every call.
			c.ruleOutStrategy(r.fact, serial, s.Name())
			lastErr = err
		default:
			// Transient: keep the strategy eligible, but let a later one answer
			// so one flaky path does not take the whole fact down.
			lastErr = err
		}
	}
	return zero, fmt.Errorf("%s: %w", r.fact, lastErr)
}

// Invalidate drops the remembered value for serial so the next Resolve re-runs
// even a Costly strategy. The ruled-out set is kept — it reflects what the
// device can do, not what it currently reports. Use this to back an explicit
// user-facing refresh.
func (r *Resolver[T]) Invalidate(c *Client, serial string) {
	if c == nil {
		return
	}
	c.factMu.Lock()
	defer c.factMu.Unlock()
	if st := c.facts[factKey(r.fact, serial)]; st != nil {
		st.cached = false
		st.val = nil
	}
}

// eligible returns the strategies that can run on this device, in registration
// (preference) order: gated by the cached Capabilities, minus the ones already
// ruled out.
func (r *Resolver[T]) eligible(ctx context.Context, c *Client, serial string) []Strategy[T] {
	caps := c.Capabilities(ctx, serial)
	ruledOut := c.ruledOutStrategies(r.fact, serial)

	out := make([]Strategy[T], 0, len(r.strategies))
	for _, s := range r.strategies {
		if ruledOut[s.Name()] {
			continue
		}
		if !r.deviceMeets(ctx, c, serial, caps, s.Requires()) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// deviceMeets reports whether the device satisfies req. An unknown SDK level
// (0) fails a MinSDK gate, which is deliberate: a strategy that needs a modern
// command must not be tried on a device we could not identify — the older path
// is the safe default.
func (r *Resolver[T]) deviceMeets(ctx context.Context, c *Client, serial string, caps *Capabilities, req Requirements) bool {
	if req.MinSDK > 0 && !caps.AndroidAtLeast(req.MinSDK) {
		return false
	}
	if req.MaxSDK > 0 && (caps == nil || caps.SDK == 0 || caps.SDK > req.MaxSDK) {
		return false
	}
	for _, bin := range req.Bins {
		if !caps.Supports(bin) {
			return false
		}
	}
	if req.Root {
		style, err := c.suStyleFor(ctx, serial)
		if err != nil || style == suUnknown {
			return false
		}
	}
	return true
}

// factState is the per-(fact, serial) bookkeeping a Resolver keeps: which
// strategies this device will never support, and the last value a Costly
// strategy produced.
type factState struct {
	ruledOut map[string]bool

	cached bool
	key    string
	at     time.Time
	val    any
}

func factKey(fact, serial string) string { return fact + "\x00" + serial }

// cachedFact returns the remembered value when it was produced under the same
// freshness key and has not aged out.
func (c *Client) cachedFact(fact, serial, freshKey string) (any, bool) {
	c.factMu.Lock()
	defer c.factMu.Unlock()
	st := c.facts[factKey(fact, serial)]
	if st == nil || !st.cached || st.key != freshKey {
		return nil, false
	}
	if time.Since(st.at) > factTTL {
		return nil, false
	}
	return st.val, true
}

// rememberFact stores the value a Costly strategy produced, against the
// freshness key it was read under. Cheap strategies are re-run instead.
func (c *Client) rememberFact(fact, serial, freshKey string, val any) {
	c.factMu.Lock()
	defer c.factMu.Unlock()
	st := c.factStateLocked(fact, serial)
	st.cached = true
	st.key = freshKey
	st.at = time.Now()
	st.val = val
}

// ruleOutStrategy marks a strategy as permanently unavailable on this device.
func (c *Client) ruleOutStrategy(fact, serial, name string) {
	c.factMu.Lock()
	defer c.factMu.Unlock()
	st := c.factStateLocked(fact, serial)
	if st.ruledOut == nil {
		st.ruledOut = map[string]bool{}
	}
	st.ruledOut[name] = true
}

// ruledOutStrategies returns a copy of the set of strategies this device has
// been proven unable to support for a fact.
func (c *Client) ruledOutStrategies(fact, serial string) map[string]bool {
	c.factMu.Lock()
	defer c.factMu.Unlock()
	st := c.facts[factKey(fact, serial)]
	if st == nil {
		return nil
	}
	ruledOut := make(map[string]bool, len(st.ruledOut))
	for k, v := range st.ruledOut {
		ruledOut[k] = v
	}
	return ruledOut
}

// factStateLocked returns the state for a fact, creating it on first use.
// Callers must hold factMu.
func (c *Client) factStateLocked(fact, serial string) *factState {
	if c.facts == nil {
		c.facts = map[string]*factState{}
	}
	k := factKey(fact, serial)
	st := c.facts[k]
	if st == nil {
		st = &factState{}
		c.facts[k] = st
	}
	return st
}

// InvalidateFacts drops every remembered fact for serial, selection included.
// Call it when the device goes away: on reconnect it may be a different build,
// newly rooted, or on another network.
func (c *Client) InvalidateFacts(serial string) {
	c.factMu.Lock()
	defer c.factMu.Unlock()
	suffix := "\x00" + serial
	for k := range c.facts {
		if len(k) > len(suffix) && k[len(k)-len(suffix):] == suffix {
			delete(c.facts, k)
		}
	}
}
