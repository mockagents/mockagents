package tenancy

import (
	"sync"
	"sync/atomic"
	"time"
)

// DefaultAuthFailuresPerMinute is how many failed authentications a single
// client IP may accumulate per minute before the middleware answers 429
// without consulting the store. Every wrong key costs one bcrypt (~50-100ms
// of CPU) by design — the timing equaliser — so with no limiter an
// unauthenticated client at a few hundred requests per second saturated
// every core (readiness audit M-09). The bucket refills continuously, so a
// legitimate client with a stale key is delayed, never locked out.
const DefaultAuthFailuresPerMinute = 30

// maxTrackedSources bounds the limiter's memory; when exceeded, buckets that
// have fully refilled are discarded.
const maxTrackedSources = 10000

type failBucket struct {
	tokens float64
	last   time.Time
}

type failureLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*failBucket
	perMinute float64
	now       func() time.Time
}

func newFailureLimiter(perMinute int) *failureLimiter {
	return &failureLimiter{
		buckets:   make(map[string]*failBucket),
		perMinute: float64(perMinute),
		now:       time.Now,
	}
}

// refillLocked brings b up to date; the caller holds l.mu.
func (l *failureLimiter) refillLocked(b *failBucket, now time.Time) {
	b.tokens += now.Sub(b.last).Minutes() * l.perMinute
	if b.tokens > l.perMinute {
		b.tokens = l.perMinute
	}
	b.last = now
}

// allow reports whether ip still has failure budget, and if not, how long
// until one token is back. It does not consume anything: only failures do.
func (l *failureLimiter) allow(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[ip]
	if !ok {
		return true, 0
	}
	l.refillLocked(b, l.now())
	if b.tokens >= 1 {
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.perMinute * float64(time.Minute))
}

// fail consumes one token for ip (creating a full bucket on first sight).
func (l *failureLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[ip]
	if !ok {
		if len(l.buckets) >= maxTrackedSources {
			for k, other := range l.buckets {
				l.refillLocked(other, now)
				if other.tokens >= l.perMinute {
					delete(l.buckets, k)
				}
			}
		}
		b = &failBucket{tokens: l.perMinute, last: now}
		l.buckets[ip] = b
	}
	l.refillLocked(b, now)
	if b.tokens > 0 {
		b.tokens--
	}
}

// authLimiter is the process-wide limiter consulted by AuthMiddleware. A nil
// pointer disables limiting.
var authLimiter atomic.Pointer[failureLimiter]

func init() { SetAuthFailureLimit(DefaultAuthFailuresPerMinute) }

// SetAuthFailureLimit configures the per-IP failed-authentication budget per
// minute for AuthMiddleware. 0 disables the limiter (not recommended outside
// tests). Called once at startup; safe to call concurrently.
func SetAuthFailureLimit(perMinute int) {
	if perMinute <= 0 {
		authLimiter.Store(nil)
		return
	}
	authLimiter.Store(newFailureLimiter(perMinute))
}

func authFailureAllowed(ip string) (bool, time.Duration) {
	if l := authLimiter.Load(); l != nil {
		return l.allow(ip)
	}
	return true, 0
}

func recordAuthFailure(ip string) {
	if l := authLimiter.Load(); l != nil {
		l.fail(ip)
	}
}
