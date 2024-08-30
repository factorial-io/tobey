package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	xrate "golang.org/x/time/rate"
)

// The maximum number of messages that can exists in the in-memory work queue.
const MemoryWorkQueueBufferSize = 1_000_000

// visitMemoryPackage is used by the in-memory implementation of the WorkQueue.
// Implementations that have a built-in mechanism to transport headers do not
// need to use this.
type visitMemoryPackage struct {
	Carrier propagation.MapCarrier
	Message *VisitMessage
}

type hostMemoryVisitWorkQueue struct {
	ID   uint32
	Name string // For debugging purposes only.

	Queue chan *visitMemoryPackage

	Limiter        *xrate.Limiter
	HasReservation bool
	IsAdaptive     bool
	PausedUntil    time.Time
}

func NewMemoryVisitWorkQueue() *MemoryVisitWorkQueue {
	return &MemoryVisitWorkQueue{
		dqueue:       make(chan *visitMemoryPackage, MemoryWorkQueueBufferSize),
		hqueues:      make(map[uint32]*hostMemoryVisitWorkQueue),
		shoudlRecalc: make(chan bool),
	}
}

type MemoryVisitWorkQueue struct {
	mu sync.RWMutex

	// This where consumers read from.
	dqueue chan *visitMemoryPackage

	// This is where message get published too. Key ist the hostname including
	// the port. It's okay to mix enqueue visits with and without authentication
	// for the same host.
	hqueues map[uint32]*hostMemoryVisitWorkQueue

	// shoudlRecalc is checked by the promoter to see if it should recalculate.
	// It is an unbuffered channel. If sending is blocked, this means there is
	// a pending notification. As one notification is enough, to trigger the
	// recalculation, a failed send can be ignored.
	shoudlRecalc chan bool
}

func (wq *MemoryVisitWorkQueue) Open(ctx context.Context) error {
	wq.startPromoter(ctx)
	return nil
}

func (wq *MemoryVisitWorkQueue) lazyHostQueue(u string) *hostMemoryVisitWorkQueue {
	p, _ := url.Parse(u)
	key := guessHost(u)

	if _, ok := wq.hqueues[key]; !ok {
		wq.mu.Lock()
		wq.hqueues[key] = &hostMemoryVisitWorkQueue{
			ID:             key,
			Name:           strings.TrimLeft(p.Hostname(), "www."),
			PausedUntil:    time.Time{},
			HasReservation: false,
			IsAdaptive:     true,
			Queue:          make(chan *visitMemoryPackage, MemoryWorkQueueBufferSize),
			Limiter:        xrate.NewLimiter(xrate.Limit(MinHostRPS), 1),
		}
		wq.mu.Unlock()
	}

	wq.mu.RLock()
	defer wq.mu.RUnlock()
	return wq.hqueues[key]
}

func (wq *MemoryVisitWorkQueue) Publish(ctx context.Context, run string, url string) error {
	defer wq.shouldRecalc() // Notify promoter that a new message is available.

	hq := wq.lazyHostQueue(url)

	// Extract tracing information from context, to transport it over the
	// channel, without using a Context.
	propagator := otel.GetTextMapPropagator()
	carrier := propagation.MapCarrier{}
	propagator.Inject(ctx, carrier)

	pkg := &visitMemoryPackage{
		Carrier: carrier,
		Message: &VisitMessage{
			ID:      uuid.New().ID(),
			Run:     run,
			URL:     url,
			Created: time.Now(),
		},
	}

	select {
	case hq.Queue <- pkg:
		slog.Debug("Work Queue: Message successfully published.", "msg.id", pkg.Message.ID)
	default:
		slog.Warn("Work Queue: full, not publishing message!", "msg", pkg.Message)
	}
	return nil
}

func (wq *MemoryVisitWorkQueue) Republish(ctx context.Context, job *VisitJob) error {
	defer wq.shouldRecalc()

	hq := wq.lazyHostQueue(job.URL)

	// Extract tracing information from context, to transport it over the
	// channel, without using a Context.
	propagator := otel.GetTextMapPropagator()
	carrier := propagation.MapCarrier{}
	propagator.Inject(job.Context, carrier)

	pkg := &visitMemoryPackage{
		Carrier: carrier,
		Message: &VisitMessage{
			ID:      job.ID,
			Run:     job.Run,
			URL:     job.URL,
			Created: job.Created,
			Retries: job.Retries + 1,
		},
	}

	select {
	case hq.Queue <- pkg:
		slog.Debug("Work Queue: Message successfully rescheduled.", "msg.id", pkg.Message.ID)
	default:
		slog.Warn("Work Queue: full, not rescheduling message!", "msg", pkg.Message)
	}
	return nil
}

// Consume returns the next available VisitJob from the default queue.
func (wq *MemoryVisitWorkQueue) Consume(ctx context.Context) (<-chan *VisitJob, <-chan error) {
	// We are unwrapping the VisitJob from the visitMemoryPackage.
	reschan := make(chan *VisitJob)
	errchan := make(chan error)

	go func() {
		for {
			select {
			case <-ctx.Done():
				slog.Debug("Work Queue: Consume context cancelled, closing channels.")

				close(reschan)
				close(errchan)
				return
			case p := <-wq.dqueue:
				// slog.Debug("Work Queue: Received message, forwarding to results channel.", "msg.id", p.Message.ID)

				// Initializes the context for the job. Than extract the tracing
				// information from the carrier into the job's context.
				jctx := context.Background()
				jctx = otel.GetTextMapPropagator().Extract(jctx, p.Carrier)

				reschan <- &VisitJob{
					VisitMessage: p.Message,
					Context:      jctx,
				}
				// slog.Debug("Work Queue: Forwarded message to results channel.", "msg.id", p.Message.ID)
			}
		}
	}()

	return reschan, errchan
}

func (wq *MemoryVisitWorkQueue) Pause(ctx context.Context, url string, d time.Duration) error {
	t := time.Now().Add(d)
	hq := wq.lazyHostQueue(url)

	if hq.PausedUntil.IsZero() || !hq.PausedUntil.After(t) { // Pause can only increase.
		hq.PausedUntil = t
	}
	return nil
}

func (wq *MemoryVisitWorkQueue) TakeRateLimitHeaders(ctx context.Context, url string, hdr *http.Header) {
	hq := wq.lazyHostQueue(url)

	if v := hdr.Get("X-RateLimit-Limit"); v != "" {
		cur := float64(hq.Limiter.Limit())

		parsed, _ := strconv.Atoi(v)
		desired := max(float64(parsed/60/60), MinHostRPS) // Convert to per second, is per hour.

		if int(desired) != int(cur) {
			slog.Debug("Work Queue: Rate limit updated.", "host", hq.Name, "now", desired, "change", desired-cur)
			hq.Limiter.SetLimit(xrate.Limit(desired))
		}
		hq.IsAdaptive = false
	}
}

// TakeSample implements an algorithm to adjust the rate limiter, to ensure maximum througput within the bounds
// set by the rate limiter, while not overwhelming the target. The algorithm will adjust the rate limiter
// but not go below MinRequestsPerSecondPerHost and not above MaxRequestsPerSecondPerHost.
func (wq *MemoryVisitWorkQueue) TakeSample(ctx context.Context, url string, statusCode int, err error, d time.Duration) {
	hq := wq.lazyHostQueue(url)

	cur := float64(hq.Limiter.Limit())
	var desired float64

	// If we have a status code that the target is already overwhelmed, we don't
	// increase the rate limit. We also don't adjust the rate limit as we
	// assume on a 429 this has already been done using TakeRateLimitHeaders

	switch {
	case statusCode == 429:
		fallthrough
	case statusCode == 503:
		return
	case statusCode == 500 || errors.Is(err, context.DeadlineExceeded):
		// The server is starting to behave badly. We should slow down.
		// Half the rate limit, capped at MinRequestsPerSecondPerHost.
		if cur/2 < MinHostRPS {
			desired = MinHostRPS
		} else {
			desired = cur / 2
		}

		if int(desired) != int(cur) && hq.IsAdaptive {
			slog.Debug("Work Queue: Lowering pressure on host.", "host", hq.Name, "now", desired, "change", desired-cur)
			hq.Limiter.SetLimit(xrate.Limit(desired))
		}
		return
	}

	if d == 0 {
		return
	}

	// The higher the latency comes close to 1s the more we want to decrease the
	// rate limit, capped at MinHostRPS. The lower the latency comes clost to 0s
	// the more we want to increase the rate limit, capped at MaxHostRPS.

	latency := min(1, d.Seconds()) // Everything above 1s is considered slow.
	desired = MaxHostRPS*(1-latency) + MinHostRPS*latency

	if int(desired) != int(cur) && hq.IsAdaptive {
		slog.Debug("Work Queue: Rate limit fine tuned.", "host", hq.Name, "now", desired, "change", desired-cur)
		hq.Limiter.SetLimit(xrate.Limit(desired))
	}
}

// startPromoter starts a the promoter goroutine that shovels message from host
// queue into the default queue. The promoter is responsible for load balancing
// the host queues.
func (wq *MemoryVisitWorkQueue) startPromoter(ctx context.Context) {
	go func() {
		slog.Debug("Work Queue: Starting promoter...")

		// Load balancing queue querying is local to each tobey instance. This should wrap
		// around at 2^32.
		var next uint32

		for {
		immediatecalc:
			// slog.Debug("Work Queue: Calculating immediate queue candidates.")
			immediate, shortestPauseUntil := wq.calcImmediateHostQueueCandidates()

			// Check how long we have to wait until we can recalc immidiate candidates.
			if len(immediate) == 0 {
				// Nothin to do yet, try again whenever
				// 1. a new message is published,
				// 2. the rate limited is adjusted
				// 3. a queue is now unpaused.
				var delay time.Duration

				if !shortestPauseUntil.IsZero() { // None may be paused.
					delay = shortestPauseUntil.Sub(time.Now())
				} else {
					delay = 60 * time.Second
				}

				slog.Debug("Work Queue: No immediate queue candidates, waiting for a while...", "delay", delay)
				select {
				case <-time.After(delay):
					// slog.Debug("Work Queue: Pause time passed.")
					goto immediatecalc
				case <-wq.shoudlRecalc:
					slog.Debug("Work Queue: Got notification to re-calc immediate queue candidates.")
					goto immediatecalc
				case <-ctx.Done():
					slog.Debug("Work Queue: Context cancelled, stopping promoter.")
					return
				}
			}

			// When we get here we have a at least one host queue that can be
			// queried, if we have multiple candidates we load balance over
			// them.
			slog.Debug("Work Queue: Final immediate queue candidates calculated.", "count", len(immediate))

			n := atomic.AddUint32(&next, 1)

			key := immediate[(int(n)-1)%len(immediate)]
			hq, _ := wq.hqueues[key]

			// FIXME: The host queue might haven gone poof in the meantime, we should
			//        check if the host queue is still there.

			slog.Debug("Work Queue: Querying host queue for messages.", "queue.name", hq.Name)
			select {
			case pkg := <-hq.Queue:
				// Now promote the pkg to the default queue.
				select {
				case wq.dqueue <- pkg:
					slog.Debug("Work Queue: Message promoted.", "msg.id", pkg.Message.ID)

					wq.mu.Lock()
					wq.hqueues[hq.ID].HasReservation = false
					wq.mu.Unlock()
				default:
					slog.Warn("Work Queue: full, dropping to-be-promoted message!", "msg", pkg.Message)
				}
			default:
				// The channel may became empty in the meantime, we've checked at top of the loop.
				// slog.Debug("Work Queue: Host queue empty, nothing to promote.", "queue.name", hq.Hostname)
			case <-ctx.Done():
				slog.Debug("Work Queue: Context cancelled, stopping promoter.")
				return
			}
		}
	}()
}

// calcImmediateHostQueueCandidates calculates which host queues are candidates
// to be queried for message to be promoted. The function modifies the host queue
// properties, so it requires a lock to be held.
//
// The second return value if non zero indicates the shortest time until a host
// queue is paused (non only the candidates). When no candidate is found, the
// callee should wait at least for that time and than try and call this function
// again.
func (wq *MemoryVisitWorkQueue) calcImmediateHostQueueCandidates() ([]uint32, time.Time) {
	// Host queue candidates that can be queried immediately.
	immediate := make([]uint32, 0, len(wq.hqueues))
	var shortestPauseUntil time.Time

	wq.mu.Lock()
	defer wq.mu.Unlock()

	// First calculate which host queues are candidates for immediate
	// querying. This is to unnecesarily hitting the rate limiter for
	// that host, as this can be expensive. More than checking the
	// PausedUntil time, or the length of the queue.
	//
	// FIXME: It might be less expensive to first check for the PausedUntil
	//        time and then check the length of the queue, depending on the
	//        underlying implementation of the work queue.
	for k, hq := range wq.hqueues {
		hlogger := slog.With("queue.name", hq.Name)
		hlogger.Debug("Work Queue: Checking if is candidate.", "len", len(hq.Queue), "now", time.Now(), "pausedUntil", hq.PausedUntil)

		if len(hq.Queue) == 0 {
			// This host queue is empty, no message to process for that queue,
			// so don't include it in the immediate list.
			continue
		}
		// hlogger.Debug("Work Queue: Host queue has messages to process.")

		if hq.PausedUntil.IsZero() {
			// This host queue was never paused before.
			// hlogger.Debug("Work Queue: Host queue is not paused.")
		} else {
			if time.Now().Before(hq.PausedUntil) {
				// This host queue is *still* paused.
				// hlogger.Debug("Work Queue: Host queue is paused.")

				// Check if this host queue is paused shorter thant the shortest
				// pause we've seen so far.
				if shortestPauseUntil.IsZero() || hq.PausedUntil.Before(shortestPauseUntil) {
					shortestPauseUntil = hq.PausedUntil
				}

				// As this host queue is still paused we don't include
				// it in the imediate list. Continue to check if the
				// next host queue is paused or not.
				continue
			} else {
				// hlogger.Debug("Work Queue: Host queue is not paused anymore.")

				// This host queue is not paused anymore, include it in
				// the list. While not technically necessary, we reset
				// the pause until time to zero, for the sake of tidiness.
				wq.hqueues[k].PausedUntil = time.Time{}
			}
		}

		// If we get here, the current host queue was either never
		// paused, or it is now unpaused. This means we can try to get a
		// token from the rate limiter, if we haven't already.
		if !hq.HasReservation {
			// hlogger.Debug("Work Queue: Host queue needs a reservation, checking rate limiter.")
			res := hq.Limiter.Reserve()
			if !res.OK() {
				hlogger.Warn("Work Queue: Rate limiter cannot provide reservation in max wait time.")
				continue
			}

			if res.Delay() > 0 {
				hlogger.Debug("Work Queue: Host queue is rate limited, pausing the queue.", "delay", res.Delay())

				// Pause the tube for a while, the limiter wants us to retry later.
				wq.hqueues[k].PausedUntil = time.Now().Add(res.Delay())

				if shortestPauseUntil.IsZero() || wq.hqueues[k].PausedUntil.Before(shortestPauseUntil) {
					shortestPauseUntil = wq.hqueues[k].PausedUntil
				}

				wq.hqueues[k].HasReservation = true
				continue
			} else {
				// Got a token from the limiter, we may act immediately.
				// hlogger.Debug("Work Queue: Host queue is not rate limited, recording as candidate :)")
				wq.hqueues[k].HasReservation = true
			}
		} else {
			// hlogger.Debug("Work Queue: Host already has a reservation.")
		}
		immediate = append(immediate, hq.ID)
	}
	return immediate, shortestPauseUntil
}

// shoudlRecalc is checked by the promoter to see if it should recalculate.
// It is an unbuffered channel. If sending is blocked, this means there is
// a pending notification. As one notification is enough, to trigger the
// recalculation, a failed send can be ignored.
func (wq *MemoryVisitWorkQueue) shouldRecalc() {
	select {
	case wq.shoudlRecalc <- true:
	default:
		// A notification is already pending, no need to send another.
	}
}

func (wq *MemoryVisitWorkQueue) Close() error {
	close(wq.dqueue)

	for _, hq := range wq.hqueues {
		close(hq.Queue)
	}
	return nil
}
