# ADR-002: DynamicScaler Worker Retirement (LIFO + Retire-at-Idle + Cooldown)

**Status:** Proposed (validated)  
**Date:** 2026-08-10  
**Deciders:** Engineering Team  
**Last validated:** 2026-08-10  

## Context

`mediahose.DynamicScaler[T]` (`src/mediahose/schedulers.go`) manages a dynamically
auto-scaling worker pool driven by queue depth. It has three concurrency defects
that we are fixing:

1. **Nondeterministic scale-down.** `removeWorker()` iterates
   `cancelFuncs map[int64]context.CancelFunc` — Go map iteration order is
   randomized, so "which worker gets retired" is arbitrary.

2. **Counter lag (optimistic accounting).** `ActiveWorkers` is decremented the
   moment a `cancel()` is issued in `removeWorker()`, but a worker only observes
   `ctx.Done()` at its next loop iteration. Capacity is reported as free while
   the worker may still be mid-job.
   > Note: this is **not** a double-decrement on the scale-down path. Current
   > `removeWorker()` deletes from `cancelFuncs` before the worker exits, so
   > `removeWorkerByIdx` no-ops. The bug is lag, not double-count.

3. **Unsafe concurrent reads (already mitigated).** Handlers previously read
   `ActiveWorkers` directly. They now call `ActiveCount()` under `RLock`
   (`src/handlers/{asset,batch,rest,video_sprite_handler,video_routes}.go`).
   This ADR replaces the counter backing that accessor with a derived slice count.

Additionally, scale/descale decisions are made on a 5s `time.After` tick from an
instantaneous `len(queue)`, which is prone to **spinup/spindown oscillation**
under bursty load. The controller also recreates a timer every loop iteration
(should be a `time.Ticker`).

## Decision

Replace the map-backed cancellable worker registry with a creation-ordered
**LIFO slice registry**, retire workers **at an idle boundary**, make the
**exit notification the single source of truth** for removal, and gate scale
decisions behind a **cooldown** — with an explicit bypass when below `MinWorkers`.

### 1. Ordered registry

```go
// WorkerSlot tracks one live worker in creation order.
// Tail of DynamicScaler.workers is the newest slot (LIFO retire target).
type WorkerSlot[T any] struct {
	Idx      int64
	Retiring bool // marked for retirement; reclaimed only on exit notify

	worker Worker[T]
	cancel context.CancelFunc
	retire chan chan bool // created & owned by the scaler; never closed
}

type DynamicScaler[T any] struct {
	WorkerFactory      func(idx int64, done chan int64) Worker[T]
	Queue              chan T
	MinWorkers         int
	MaxWorkers         int
	ScaleUpThreshold   int
	ScaleDownThreshold int
	ScaleSigChan       chan struct{}
	Name               string

	CheckInterval time.Duration // scale() cadence; default 5s
	ScaleCooldown time.Duration // min time between scale-up/down; default 30s
	RetireGrace   time.Duration // wait before cancel-nudge; default 2s

	mu              sync.RWMutex
	workers         []*WorkerSlot[T] // creation order → tail = newest
	nextIdx         int64
	lastScale       time.Time
	workerCloseChan chan int64
}
```

- `nextIdx` only increases → **append = newest = LIFO retire target**.
- `cancelFuncs` and the separate `ActiveWorkers int` disappear.
- Authoritative membership = the slice. Live capacity for scale math =
  non-`Retiring` slots. Hard cap for goroutines = `len(workers)` (see §Capacity).

### 2. Retire-at-idle — single blocking select (corrected)

**Validation finding:** the earlier dual-select-with-`default` design was wrong.
An idle worker blocked on `case job := <-jobQueue` would **not** see a pending
retire request until it processed another job. That defeats retire-at-idle.

Correct pattern: one `select` with `ctx`, `RetireCh`, and the job queue — no
`default`. The worker is idle while blocked in that select; a retire request is
accepted immediately without taking another job.

```go
func (fw *FetchWorker) Work(ctx context.Context, jobQueueChan <-chan *Job) {
	defer func() { fw.CloserChan <- fw.Idx }()

	for {
		select {
		case <-ctx.Done():
			log.Println("FetchWorker exiting...")
			return

		case retireReq := <-fw.RetireCh:
			// Idle boundary: blocked here only when not mid-job.
			// Ack via send (do not close — scaler owns ack).
			// Use default so a timed-out scaler closing ack cannot panic us.
			select {
			case retireReq <- true:
			default:
			}
			log.Println("FetchWorker retiring at idle boundary...")
			return

		case job := <-jobQueueChan:
			// ...existing process body unchanged...
		}
	}
}
```

A `nil` `RetireCh` is safe: a receive on a nil channel never becomes ready, so
unwired workers simply never take the retire case (legacy ctx-cancel only).

Workers that opt in implement:

```go
type RetireAwareWorker interface {
	SetRetireCh(chan chan bool)
}
```

### 3. Scale-down marks; exit reclaims

```go
func (ds *DynamicScaler[T]) retireWorkerLocked() {
	wsl := ds.newestNonRetiring() // scan tail → head
	if wsl == nil {
		return
	}
	wsl.Retiring = true
	log.Println(ds.Name, "retiring worker", wsl.Idx)

	if _, ok := wsl.worker.(RetireAwareWorker); !ok || wsl.retire == nil {
		wsl.cancel() // legacy: ctx-cancel retire
		return
	}

	ack := make(chan bool, 1)
	select {
	case wsl.retire <- ack:
		// Request queued. Worker will ack when it next parks idle.
	default:
		// Buffer full (should not happen: one retire per slot). Nudge.
		wsl.cancel()
		return
	}

	grace := ds.RetireGrace
	go func() {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-ack:
			log.Println(ds.Name, "worker", wsl.Idx, "acked retirement")
			// Do not close(ack): worker may race a late send; GC is enough
			// for a 1-buffer chan that nobody else receives from.
		case <-timer.C:
			// Still mid-job after grace. Cancel is a *nudge*, not the reclaim.
			// Reclaim still happens only on CloserChan exit notify.
			wsl.cancel()
		}
	}()
}
```

**Validation finding (ack close race):** the earlier `defer close(ack)` after
grace timeout races the worker's `retireReq <- true` → **send on closed channel
panic**. Fix: **never close the one-shot ack**. Scaler created it; nobody else
needs it closed. Buffered(1) + worker `select { case send: default: }` makes
both sides non-panicking if the other side has moved on.

Slot stays in `workers` with `Retiring=true` until `CloserChan <- Idx` arrives;
`removeWorkerByIdx` drops it. **Removal only on actual exit.**

### 4. Capacity rules (corrected)

| Metric | Definition | Used for |
|--------|------------|----------|
| `live` | `count !Retiring` | scale-down floor, `ActiveCount()`, "can we retire?" |
| `total` | `len(workers)` | **hard** scale-up cap vs `MaxWorkers` |
| gauge | `total` (+ optional `live`) | ops visibility |

**Validation finding:** using only `live < MaxWorkers` for scale-up lets
`live + retiring > MaxWorkers` (goroutine leak under slow drain). Correct:

```go
// scale-up:
if queueLen > ds.ScaleUpThreshold && total < ds.MaxWorkers && live < ds.MaxWorkers {
    add...
}
// scale-down:
if queueLen < ds.ScaleDownThreshold && live > ds.MinWorkers {
    retire...
}
// floor recovery (cooldown BYPASSED):
if live < ds.MinWorkers && total < ds.MaxWorkers {
    add...
}
```

### 5. Cooldown with MinWorkers bypass (corrected)

```go
func (ds *DynamicScaler[T]) scale(ctx context.Context) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	queueLen := len(ds.Queue)
	live := countLive(ds.workers)
	total := len(ds.workers)

	// Floor recovery always allowed — do not leave the pool empty after crashes.
	belowFloor := live < ds.MinWorkers && total < ds.MaxWorkers

	if !belowFloor && ds.ScaleCooldown > 0 && time.Since(ds.lastScale) < ds.ScaleCooldown {
		return
	}

	// ...gauges...

	switch {
	case queueLen > ds.ScaleUpThreshold && total < ds.MaxWorkers:
		ds.addWorkerLocked(ctx)
		ds.lastScale = time.Now()
	case queueLen < ds.ScaleDownThreshold && live > ds.MinWorkers:
		ds.retireWorkerLocked()
		ds.lastScale = time.Now()
	case belowFloor:
		ds.addWorkerLocked(ctx)
		ds.lastScale = time.Now()
	}
}
```

**Validation finding:** applying cooldown to the `active < MinWorkers` top-up
path can leave the pool empty for the full cooldown after a crash storm. Floor
recovery must bypass cooldown.

Defaults in `BootStrapDynamicScalerFrom`:

| Field | Default | Notes |
|-------|---------|--------|
| `CheckInterval` | `5s` | use `time.NewTicker`, not `time.After` in the loop |
| `ScaleCooldown` | `30s` | **must be non-zero in prod**; `0` disables hysteresis |
| `RetireGrace` | `2s` | cancel-nudge only; reclaim still on exit |
| `workerCloseChan` buf | `MaxWorkers` | was `1` — multi-exit could stall |

### 6. Controller loop — ticker, not `time.After`

```go
go func() {
	ticker := time.NewTicker(ds.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			ds.shutdown()
			return
		case idx := <-ds.workerCloseChan:
			ds.removeWorkerByIdx(idx)
			ds.scale(ctx) // floor bypass handles crash refill
		case <-ds.ScaleSigChan:
			ds.scale(ctx)
		case <-ticker.C:
			ds.scale(ctx)
		}
	}
}()
```

## Implementation Details

### Worker interface + retire opt-in

```go
type Worker[T any] interface {
	Work(ctx context.Context, jobQueueCh <-chan T)
}

type RetireAwareWorker interface {
	SetRetireCh(chan chan bool)
}
```

`Worker` signature is unchanged. Opt-in is additive.

### FetchWorker (primary server path — must opt in)

```go
type FetchWorker struct {
	Idx        int64
	CloserChan chan int64
	RetireCh   chan chan bool
}

func (fw *FetchWorker) SetRetireCh(ch chan chan bool) { fw.RetireCh = ch }
// Work: single select as above
```

`cmd/server` image + video scalers use `FetchWorker` → this is the path that
gets true retire-at-idle.

### BatchWorker / EnhancedBatchWorker / ffmpeg workers

| Implementer | Used with DynamicScaler? | Opt-in required for v1? |
|-------------|--------------------------|-------------------------|
| `FetchWorker` | yes (`cmd/server`) | **yes** |
| `BatchWorker` | rarely (Lambda runs it bare) | optional |
| `EnhancedBatchWorker` | yes (`enhanced_video_routes`) | optional (legacy cancel) |
| `VideoWorker` / `BatchVideoWorker` | yes (`video_routes`) | optional (legacy cancel) |

Non-opt-in workers still scale down via `wsl.cancel()`; they keep finishing the
current job (ctx checked at loop top) then exit. That is "retire at next loop
boundary", slightly weaker than the handshake but acceptable for v1.

> Earlier draft contradicted itself by both requiring and waiving BatchWorker
> opt-in. Clarified above.

### Bootstrap / ActiveCount / add / drop

```go
func BootStrapDynamicScalerFrom[T any](scaler *DynamicScaler[T]) *DynamicScaler[T] {
	buf := scaler.MaxWorkers
	if buf < 1 {
		buf = 1
	}
	if scaler.MinWorkers > buf {
		buf = scaler.MinWorkers
	}
	scaler.workerCloseChan = make(chan int64, buf)

	if scaler.CheckInterval <= 0 {
		scaler.CheckInterval = 5 * time.Second
	}
	if scaler.ScaleCooldown < 0 {
		scaler.ScaleCooldown = 0 // explicit disable
	} else if scaler.ScaleCooldown == 0 {
		scaler.ScaleCooldown = 30 * time.Second // safe default
	}
	if scaler.RetireGrace <= 0 {
		scaler.RetireGrace = 2 * time.Second
	}
	return scaler
}

func (ds *DynamicScaler[T]) ActiveCount() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return countLive(ds.workers)
}

func (ds *DynamicScaler[T]) addWorkerLocked(ctx context.Context) {
	worker := ds.WorkerFactory(ds.nextIdx, ds.workerCloseChan)
	workerCtx, cancel := context.WithCancel(ctx)
	retire := make(chan chan bool, 1) // scaler-owned; never closed

	if rw, ok := worker.(RetireAwareWorker); ok {
		rw.SetRetireCh(retire)
	}

	wsl := &WorkerSlot[T]{
		Idx: ds.nextIdx, worker: worker, cancel: cancel, retire: retire,
	}
	ds.workers = append(ds.workers, wsl)
	ds.nextIdx++
	go worker.Work(workerCtx, ds.Queue)
}

func (ds *DynamicScaler[T]) removeWorkerByIdx(idx int64) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if wsl := ds.findSlot(idx); wsl != nil {
		ds.dropSlot(idx)
		log.Println(ds.Name, "worker", wsl.Idx, "exited; reclaimed")
	}
}

func (ds *DynamicScaler[T]) shutdown() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	for _, wsl := range ds.workers {
		wsl.cancel()
	}
	ds.workers = nil
	// do not close workerCloseChan or per-slot retire chans
}
```

### Channel ownership

| Channel | Created by | Closed by | Why |
|---------|------------|-----------|-----|
| `workerCloseChan` | scaler | **never** | send-on-closed panic if workers drain after close |
| `WorkerSlot.retire` | scaler | **never** | nil-receive would hand worker a nil ack |
| one-shot `ack` | scaler (retire path) | **never** (GC) | close races worker send → panic; buffer 1 is enough |

"Close where created" is applied where safe. Long-lived coordination channels
are owned by the scaler and left for GC; that is deliberate, not an oversight.

### Callers

- `cmd/server/main.go` factory signatures unchanged; optionally set
  `ScaleCooldown` / `CheckInterval` on the scaler literals.
- Handlers keep `Scaler.ActiveCount()` (already landed).
- Prefer `ScaleSigChan: make(chan struct{}, 1)` (as `video_routes` already does)
  so handler scale pings never block the request goroutine.

## Alternatives Considered

### A. Keep map registry + `ActiveWorkers` counter — rejected
Does not fix nondeterministic removal or counter lag.

### B. `atomic.Int64` counter — rejected
More churn, no win: registry mutations already require `mu`. Derived
`countLive(workers)` under the same lock is simpler and cannot drift from
membership.

### C. Dual-select + `default` idle poll — rejected (was in draft v1)
Idle workers blocked on the job case never saw retire requests. Wrong.

### D. `defer close(ack)` after grace — rejected (was in draft v1)
Races worker ack send → panic.

### E. Cooldown on all scale paths including floor — rejected (was in draft v1)
Crash storm can empty the pool for the full cooldown window.

## Consequences

### Positive
- Deterministic LIFO retirement (newest non-retiring worker first).
- Exit notify is the only reclaim event → no counter lag vs membership.
- True retire-at-idle for opt-in workers (single blocking select).
- Cooldown damps oscillation; floor bypass keeps MinWorkers honest.
- `MaxWorkers` remains a hard goroutine cap (`total`).
- Backward compatible: `Worker` interface unchanged; non-opt-in workers keep
  legacy cancel path.
- Fixes the old unsynchronized `ActiveWorkers` read via `ActiveCount()`.

### Negative / residual risks
- `Retiring` workers still occupy a `total` slot until exit → scale-up may wait.
  Desired smoothing; bounded by job duration + `RetireGrace` cancel-nudge.
- Cancel-nudge after grace **can** interrupt in-flight work that respects `ctx`
  (image path checks `ctx` at process start). Doc must not claim "never
  interrupts mid-job" while grace cancel exists. Accurate claim: **prefer**
  idle retire; cancel is a bounded fallback.
- `ScaleCooldown` default 30s must be tuned per workload.
- Slightly more state (`Retiring`, handshake) than the old cancel map.

## Validation checklist (pre-implement)

- [x] Context defects match live `schedulers.go` (map iteration, counter lag, race)
- [x] Double-decrement claim corrected → lag only
- [x] Worker loop is single blocking select (no default spin / missed retire)
- [x] Ack is never closed (no send-on-closed race)
- [x] Scale-up caps on `total` (not only `live`)
- [x] Cooldown bypass when `live < MinWorkers`
- [x] `ScaleCooldown` default non-zero
- [x] `workerCloseChan` buffer ≥ MaxWorkers
- [x] Controller uses `time.Ticker`
- [x] BatchWorker opt-in scope clarified (optional; FetchWorker required)
- [x] Channel ownership table matches "close where created" exceptions
- [x] Positive claims do not overstate mid-job safety

## Rollout

1. Task-1 `ActiveCount()` + handler edits — **done**.
2. Apply this validated design to `src/mediahose/schedulers.go`.
3. Opt `FetchWorker` into `RetireAwareWorker`; leave others on legacy cancel
   unless needed.
4. Wire `ScaleCooldown` / `CheckInterval` / `RetireGrace` from config (or scaler
   literals in `cmd/server`).
5. Buffer `ScaleSigChan` (cap 1) on server scalers.
6. Race test: `go test -race ./src/mediahose/...` + a small scaler unit test:
   - LIFO order under sequential scale-down
   - no goroutines above MaxWorkers with slow jobs
   - floor recovery after forced worker exit during cooldown

---
**Risk Level:** Low–Medium  
**Estimated Implementation:** 1–2 days  
**Review Date:** 2026-08-13