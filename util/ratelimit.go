package util

import (
	"container/list"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("ratelimit")

// A RateLimiter limits the rate at which an action can be performed.  It
// applies neither smoothing (like one could achieve in a token bucket system)
// nor does it offer any conception of warmup, wherein the rate of actions
// granted are steadily increased until a steady throughput equilibrium is
// reached.
type RateLimiter struct {
	limit        int
	interval     time.Duration
	mtx          sync.Mutex
	times        list.List
	parallelChan chan bool
	coolDown     bool
}

// ErrExceeded should be returned if we need to rerun the function
var (
	ErrExceeded = errors.New("Rate-Limit Exceeded")
	ErrNotFound = errors.New("Not Found")
	ErrHTTP     = errors.New("HTTP error")
)

// NewRateLimiter creates a new rate limiter for the limit and interval.
func NewRateLimiter(limit int, interval time.Duration, parallelCount int) *RateLimiter {
	lim := &RateLimiter{
		limit:        limit,
		interval:     interval,
		parallelChan: make(chan bool, parallelCount),
	}
	lim.times.Init()
	return lim
}

// Wait blocks if the rate limit has been reached.  Wait offers no guarantees
// of fairness for multiple actors if the allowed rate has been temporarily
// exhausted.
func (r *RateLimiter) Wait() {
	for {
		ok, remaining := r.Try()
		if ok {
			break
		}
		time.Sleep(remaining)
	}
}

// ForceWait is forcing rate limit if we have an external cause
// (like Response from API).
func (r *RateLimiter) ForceWait() {
	r.mtx.Lock()
	now := time.Now()
	for r.times.Len() < r.limit {
		r.times.PushBack(now)
	}
	r.mtx.Unlock()

	r.Wait()
}

// Try returns true if under the rate limit, or false if over and the
// remaining time before the rate limit expires.
func (r *RateLimiter) Try() (ok bool, remaining time.Duration) {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	now := time.Now()
	if l := r.times.Len(); l < r.limit {
		r.times.PushBack(now)
		return true, 0
	}
	frnt := r.times.Front()
	if diff := now.Sub(frnt.Value.(time.Time)); diff < r.interval {
		return false, r.interval - diff
	}
	frnt.Value = now
	r.times.MoveToBack(frnt)
	return true, 0
}

// CoolDown is checking HTTP headers if we need to wait
func (r *RateLimiter) CoolDown(headers http.Header) {
	if len(headers) == 0 {
		return
	}
	if retryAfter, exists := headers["Retry-After"]; exists {
		if retryAfter == nil {
			return
		}

		coolDown, err := strconv.Atoi(retryAfter[0])
		if err != nil || r.coolDown {
			return
		}

		r.mtx.Lock()
		log.Debugf("Met a cooldown, sleeping for %#v seconds. Headers: %#v", coolDown, headers)

		// Marking we are going to sleep, so other can see and just return an error
		// to avoid many sleeps
		r.coolDown = true

		// Sleeping for requested seconds, but if we get 0, sleeping for some time
		timeout := time.Duration(coolDown) * time.Second
		if coolDown == 0 {
			timeout = time.Duration(300) * time.Millisecond
		}
		time.Sleep(timeout)
		r.mtx.Unlock()

		r.ForceWait()
		r.coolDown = false
	}
}

// Call ...
func (r *RateLimiter) Call(f func() error) {
	// Count simultaneous calls
	r.Enter()
	defer r.Leave()

	// Checking for burst rate
	r.Wait()

	tries := 0
	for {
		err := f()
		// If fail occur, we should rerun
		if err == nil || err != ErrExceeded || tries >= 2 {
			break
		}
		tries++
	}
}

// Enter blocks parallen channel for simultaneous connections limitation
func (r *RateLimiter) Enter() {
	r.parallelChan <- true
}

// Leave removes channel usage
func (r *RateLimiter) Leave() {
	<-r.parallelChan
}
