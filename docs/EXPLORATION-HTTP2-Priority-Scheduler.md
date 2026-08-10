# Exploration: HTTP/2 Cross-Stream Priority Scheduler

**Status:** Exploration / learning project — not scheduled, not an ADR decision
**Date:** 2026-08-10
**Origin:** investigating Cloudflare's "Parallel Streaming of Progressive Images"
(https://blog.cloudflare.com/parallel-streaming-of-progressive-images/) against
this codebase's `ProgressiveStreamEncoder` (`src/encoders/encoders.go:100`)

## 1. The problem

A page with many images (`<img>` tags) causes the browser to open many
concurrent HTTP requests to the same origin. Over HTTP/2 these become
concurrent **streams** multiplexed on one TCP connection. Two separate
things determine how "good" the loading experience is:

1. **Content structure** — within *one* image's response, is the data
   ordered so the browser can render something useful before the whole file
   arrives? (Progressive JPEG: size header first, then a low-res-usable
   scan, then refinement data.)
2. **Cross-stream scheduling** — across *all the images currently loading*,
   whose bytes get put on the wire next? If the server just answers
   requests in whatever order handlers finish, image A's full body can hog
   the connection while image B's browser-blocking size header sits queued
   behind it, unrendered.

This codebase already does (1) — `ProgressiveStreamEncoder` chunks a single
image into header/preview/remainder writes. It does **not** do (2) — nothing
in the stack currently controls write order *across* concurrently open
image requests. That's the gap this document is about.

## 2. Why this is harder than it sounds — what we found investigating it

This section is the actual research trail from digging into each layer this
project already touches, because the answer differs by layer and it's easy
to assume the wrong one has the capability.

### Two unrelated concepts to keep separate
- **Stream count**: the *browser* decides how many streams open (one per
  request it makes) and *when* — the server has no say and needs no advance
  knowledge of "what images exist." This ruled out an early hypothesis that
  Cloudflare needed R2/S3 bucket introspection to know what to prioritize —
  it doesn't; it reacts to requests already in flight.
- **Prefetch/preload** (`Link: rel=preload`, 103 Early Hints) is a *different*
  mechanism — telling the browser to start requesting something before it's
  discovered it. Irrelevant here; by the time cross-stream scheduling
  matters, the browser has already made all the requests.

### The actual lever: the HTTP/2 write scheduler
Once N streams are open concurrently, one component in the stack owns the
single shared connection's write order: the **write scheduler**, sitting
below the request/response abstraction. It decides, for each write
opportunity, which stream's buffered bytes go out next. Neither application
handler code nor most reverse-proxy config layers can see across sibling
streams — only whoever multiplexes the connection can.

### What each stack in this project's toolbox actually offers, checked directly against source (not docs summaries)

| Layer | Capability | Evidence |
|---|---|---|
| **Go `net/http` + `x/net/http2`** | Had a pluggable `WriteScheduler` interface (`OpenStream`/`CloseStream`/`AdjustStream`/`Push`/`Pop`), but it's now **deprecated** (commit `8afa12f`, tracking golang/go#67817). Maintainers' own words: *"provides too much visibility into implementation internals, is difficult to use, and limits our ability to improve performance."* Also: **no public API exists to get the current stream ID inside an `http.Handler`** — confirmed via pkg.go.dev — so even while the hook existed, correlating "which handler goroutine owns which stream" for reprioritization was an unsolved, non-trivial problem. | pkg.go.dev/golang.org/x/net/http2, github.com/golang/net commit 8afa12f |
| **nginx** | Has its own internal RFC 7540 weight/dependency tree (`ngx_http_v2_node_t` with `weight`/`rel_weight` fields, in `src/http/v2/ngx_http_v2.h`) used to order `h2c->last_out`, its output frame queue (`ngx_http_v2_send_output_queue` in `ngx_http_v2.c`). **Not exposed as a module API** — third-party modules (Lua or native C) hook nginx's request-processing *phases* (rewrite/access/content/header-filter/body-filter), a layer above the connection-level scheduler. Reaching the priority tree means patching nginx core, not writing a module. Also: no RFC 9218 (Extensible Priorities) support found anywhere in nginx's changelog — still RFC 7540-only. | Cloned `nginx/nginx`, read `src/http/v2/ngx_http_v2.h` + `ngx_http_v2.c` directly; `lua-nginx-module` README has zero mentions of HTTP/2/priority |
| **fasthttp / Fiber** | fasthttp core has **no HTTP/2 at all** (its own FAQ: *"HTTP/2.0 support is in progress"*). The separate `fasthttp/http2` package is a from-scratch, "under construction" H2 implementation that does parse the RFC 7540 `PRIORITY` frame (`priority.go`) — but the parsed `weight` value is **never read again anywhere in the codebase** (grepped `serverConn.go`, `streams.go`). It has no scheduler at all, not even a buggy one. | Cloned `fasthttp/http2`, grepped for `weight`/`Weight` usage outside the frame struct — zero hits |
| **Cloudflare** | Built on their own from-scratch H2 implementation (Pingora). `cf-priority-change` is their proprietary extension of the underlying idea. | blog post itself |

**Correction — this capability is not unique to Cloudflare.** The table above
only checked the three stacks already in this project's toolbox (Go, nginx,
fasthttp). Broadening the search turned up real, working, non-Cloudflare
implementations — see below. The accurate conclusion is: **none of the
stacks this project currently uses** implement cross-stream H2 scheduling
well, not that nobody does.

### Real reference implementations found (H2O, nghttp2, Tempesta FW)

Source: [Tempesta Tech — HTTP/2 Streams Prioritization](https://tempesta-tech.com/knowledge-base/HTTP2-streams-prioritization/)

| Implementation | Approach | Verdict |
|---|---|---|
| **H2O** (`lib/http2/scheduler.c`, MIT-licensed) | O(1) "Array of Queue": a tree of 64-entry arrays per level, streams inserted by dependency+weight, a precomputed weight→offset table (256 entries covering weights 1-256) plus a bit array for fast highest-priority lookup. | **Fast but not fair.** Tempesta simulated H2O's scheduler against theoretical Weighted Fair Queueing (WFQ) across 256 streams (real sites like slack.com run 100-200 concurrent streams). Only 2 of 256 streams got ideal scheduling; 5% were off by 100% from the WFQ-ideal distribution. |
| **nghttp2** (used by Envoy, Apache HTTPD) | Implements correct WFQ and already supports RFC 9218 (Extensible Priorities) — the one implementation of the modern spec found anywhere in this research. | **Fair but not fast.** Plain sorted array with insertion-driven reallocation, O(n) traversal. |
| **Tempesta FW** (in-kernel HTTP accelerator) | Chose WFQ (for fairness, like nghttp2) but replaced the priority-queue data structure with HAProxy's `ebtree`, after benchmarking Fibonacci heap, RB-tree, and insertion-sorted array for their target range of 100-1000 concurrent streams. In their own words: *"We chose to use WFQ, like nghttp2, to get fairness, but use a better data structure for the priority queues... We analyzed some of them (e.g. Fibonacci heap, RB-tree, insertion sorted array etc) and found that the HAProxy's ebtree provides the best performance (at least x2 faster than the closest in performance Fibonacci heap) on small data (about 100 to 1000 streams in a queue) to pick a minimum item and reinsert it."* | **Fair and fast** — but note the cost: their scheduler runs in-kernel with TCP-integrated delayed SKB queuing (implemented against the Linux TCP stack itself, not user-space), and their own PR notes ~30% throughput degradation vs. their pre-scheduler baseline (291k→205k req/s) as the cost of correctness over raw speed. PR: [tempesta-tech/tempesta#1973](https://github.com/tempesta-tech/tempesta/pull/1973) (merged). |

**Revised conclusion of the research phase:** the mechanism is achievable —
H2O proves an O(1) approach exists and is MIT-licensed (legally adaptable),
nghttp2 proves WFQ-correctness is a solved algorithm, and Tempesta proves
the "fast AND fair" combination is achievable by pairing WFQ with a good
priority-queue data structure (ebtree over Fibonacci heap, specifically for
the 100-1000-stream range this project would realistically see). The real,
measured tradeoff — not hypothetical — is **speed vs. fairness**, and the
fix for that tradeoff is the *data structure* backing the scheduler, not the
scheduling algorithm itself. This directly informs §5's build stages below:
stage 1 should start from H2O's array-of-queue design (simplest to reason
about, reference implementation available to read), with a clear-eyed
understanding — from Tempesta's own benchmark — that it will exhibit real
unfairness at scale, and a WFQ+ebtree upgrade path exists if that turns out
to matter.

## 3. What we're building, and why

**What:** A minimal HTTP/2 server built directly on `golang.org/x/net/http2.Framer`
(the raw frame reader/writer — bypassing `http2.Server` and its deprecated
`WriteScheduler` entirely) that implements a **strict phase-priority
scheduler**: across all currently-open streams, all `header`-phase chunks
are written before any `preview`-phase chunk, before any `body`-phase chunk.

**Why build it instead of using something off the shelf:** because nothing
off the shelf does this (§2). The value isn't "get this working as fast as
possible" — it's understanding the mechanism by owning it, in a stack this
project could plausibly deploy without depending on Cloudflare specifically
being in front of it.

**Why this is learning-project scope, not a production plan:** a
correct, production-grade H2 stack needs full flow-control accounting
(connection- and stream-level `WINDOW_UPDATE` handling), HPACK correctness,
proper `SETTINGS` negotiation, error/`RST_STREAM` handling, and TLS/ALPN —
each a source of real bugs even in mature implementations (it's *why* Go
retired its own attempt at just the scheduling piece). This doc scopes a
staged build that proves the mechanism first, before any of that hardening.

## 4. How we'll build it — architecture

Two goroutines per connection instead of Go's per-request-goroutine model —
this is the structural choice that makes scheduling possible at all, since
only a single goroutine that can see *every* open stream can decide write
order between them.

```
TLS/plaintext Listener
  └─ per connection:
       readLoop(conn)   — the only goroutine calling Framer.ReadFrame
       writeLoop(conn)  — the only goroutine writing to the socket
```

### readLoop — owns frame parsing, stream creation
- Reads the H2 client preface, exchanges initial `SETTINGS`.
- On `HEADERS`: HPACK-decode, create a `stream` struct keyed by the frame's
  stream ID (no correlation guesswork — the ID is unambiguous right here,
  unlike inside a handler goroutine later), spawn a handler goroutine and
  hand it a direct pointer to its `stream`.
- On `WINDOW_UPDATE`: replenish the relevant stream's/connection's send
  window, wake the writeLoop.

### stream state — where the content-priority phases live
```go
type phase int
const (
    phaseHeader phase = iota // image dimensions/metadata — highest priority
    phasePreview               // enough bytes for a usable low-res paint
    phaseBody                   // remainder — lowest priority
)

type stream struct {
    id      uint32
    phase   phase
    pending [][]byte // chunks queued for the current phase
    sendWnd int32    // this stream's H2 flow-control window
}
```
Handler goroutines never write to the socket. They call
`stream.pushPhase(phaseHeader, bytes)` / `pushPhase(phasePreview, ...)` /
`pushPhase(phaseBody, ...)`, each call advancing `stream.phase` and waking
the writeLoop. This is where `ProcessImageWithResponse` /
`WebpProcessor`/`JpegProcessor`/`PngProcessor` would plug in unchanged —
only the sink changes, from `http.ResponseWriter.Write` to `pushPhase`.

### writeLoop — the actual scheduler
On every wake-up (new pending bytes, phase change, or window update),
sweep **all open streams**, phase by phase:
```
for phase in [header, preview, body]:
    for each open stream currently at this phase:
        if stream has flow-control budget and pending bytes:
            write one DATA frame chunk, deduct from both stream and
            connection send windows
```
This single sweep *is* the feature: "send headers of image 1, 2, 3…
before previews of any, before full bodies of any," across every
concurrently open request on the connection — the exact behavior from the
Cloudflare post, achievable because one goroutine sees every stream's phase
simultaneously.

### Flow control
Every `DATA` write must respect both the connection-level and the
per-stream send window (replenished by peer `WINDOW_UPDATE` frames). This
bookkeeping is what `http2.Server`'s built-in machinery handled for free,
and it's the most bug-prone part of any hand-rolled H2 stack — the primary
reason to stage the build (§5) rather than write it all at once.

## 5. Build stages (parts breakdown)

1. **Toy, no real flow control.** Plaintext h2c, 2-3 hardcoded fake
   streams with tiny fixed payloads (well under the default 65535-byte
   window, so `WINDOW_UPDATE` handling can be skipped safely for this stage
   only). Goal: prove the phase-sweep interleaving is real and visible —
   run a client that opens 3 streams concurrently and logs DATA frames in
   receipt order; the log should show all 3 headers, then all 3 previews,
   then all 3 bodies, interleaved across streams rather than stream-by-
   stream.
2. **Real flow control.** Add proper connection- and stream-level window
   accounting driven by actual `WINDOW_UPDATE` frames, so larger payloads
   (real image sizes) work without violating the protocol.
3. **Wire in the real pipeline.** TLS/ALPN termination, HPACK request
   parsing for real paths/query params, and swap the fake phase payloads
   for actual calls into `ProcessImageWithResponse` and the
   `Webp/Jpeg/PngProcessor` functions from `src/mediahose`.

## 6. Open questions / risks to revisit before going past stage 1

- Starvation: a stream stuck at zero flow-control window in `phaseHeader`
  souldn't be allowed to block a *different* stream's `phasePreview` bytes
  from going out — the sweep design in §4 already handles this (it skips
  blocked streams and continues the loop), but needs a test proving it
  under real windows in stage 2.
- HPACK correctness (dynamic table state, huffman encoding) is a common
  source of real-world H2 bugs; stage 3 is where this actually gets
  exercised for the first time with non-trivial request headers.
- This whole exploration assumes self-terminating H2 (no CDN in front). If
  a CDN ever sits in front of this in production, its own scheduler
  (hopefully as good as Cloudflare's) takes over and this becomes moot for
  that deployment — worth re-checking before investing past stage 1.
- Fairness at scale: our phase-only sweep (§4) is closer in spirit to H2O's
  fast-but-unfair approach than to WFQ — it has no notion of *within-phase*
  fairness across streams competing for the same priority band. Per the H2O
  benchmark above, this is where real inaccuracy would show up once past a
  handful of test streams. If that matters for this project's actual scale,
  the fix (per Tempesta's findings) is a WFQ-correct priority queue backing
  each phase's stream set — worth prototyping with a plain heap first before
  reaching for ebtree, and only justified once stage 1/2 prove the phase
  mechanism works at all.
