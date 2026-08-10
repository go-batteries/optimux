# ADR-003: Streaming Image Load/Export via govips (Deferred)

**Status:** Deferred — revisit once govips tags a release containing streaming I/O
**Date:** 2026-08-10
**Deciders:** Engineering Team
**Origin:** [davidbyttow/govips#476](https://github.com/davidbyttow/govips/issues/476) (raised by @amitavaghosh2)

## Context

The image pipeline currently buffers full images in memory at every stage:

1. **Load** — `LoadImageFromURLWithCache` / `LoadImageFromTmpFS`
   (`src/mediahose/loaders.go`) download/read the source fully before handing
   govips a `*vips.ImageRef`.
2. **Export** — `WebpProcessor` / `JpegProcessor` / `PngProcessor`
   (`src/mediahose/vipsprocessor.go`) call `img.ExportWebp/Jpeg/Png(params)`,
   which return a fully-materialized `[]byte`.
3. **Pipeline** — `ProcessImageWithResponse` (`src/mediahose/processors.go`)
   and `FetchWorker.Work` (`src/mediahose/schedulers.go`) pass that `[]byte`
   end to end.
4. **Sink** — `src/encoders/encoders.go`: `ResponseOpts.Data []byte` feeds
   either chunked HTTP writes (`StreamEncoder`, `ProgressiveStreamEncoder` —
   these chunk an already-fully-buffered slice, so they don't reduce peak
   memory) or `S3Uploader.Upload` (`bytes.NewReader(opt.Data)` into
   `s3.PutObjectInput.Body`, which itself accepts any `io.Reader`).

For dynamic compression of non-standard sizes, this means peak memory is
proportional to full encoded image size, multiplied by concurrent workers.

### What we found investigating #476

- When the issue was opened (2025-04) and answered (2025-07), govips had no
  streaming support. The maintainer pointed to
  [vipsgen](https://github.com/cshum/vipsgen) — a different, code-generated
  libvips binding — as the only option with `io.Reader`/`io.Writer`-based
  Source/Target streaming.
- **govips has since added native streaming**, merged 2026-08-07/08:
  - `vips.LoadImageFromReader(r io.Reader, params *ImportParams) (*ImageRef, error)`
  - `(*ImageRef).SaveToWriter(w io.Writer, format ImageType, params *ExportParams) error`
  - Format-specific variants: `SaveToWriterJpeg/Png/Webp/Tiff/Heif/Gif`
  - Plus `TranscodeStream`, `SetStreamScratchDir`, `SetStreamDiscThreshold`.
- This supersedes the need to migrate to vipsgen — the fix landed in the
  library already in use (`go.mod` currently pins `v2.16.0`).
- **Not yet in a tagged release.** Latest tag is `v2.18.0` (2026-04-01);
  the streaming commits are four months newer and only on `master`. No
  changelog/stability guarantee yet — API could still shift before release.

## Decision

**Defer the migration.** Do not pin to an untagged govips commit for the
production image pipeline. Pick this up once govips cuts a release that
includes the streaming API, then:

1. Bump `go.mod` to that release.
2. Swap `loaders.go` load calls to `vips.LoadImageFromReader`.
3. Swap `vipsprocessor.go` `Export*` calls to `SaveToWriter*`.
4. Reshape the buffered `[]byte` interfaces to stream through:
   - `MediaProcessor.Process(ctx, job) ([]byte, error)` → reader/writer-based
   - `encoders.Encoder func(ctx, opts, w) error` / `ResponseOpts.Data []byte`
     → `io.Reader`/direct `io.Writer` pass-through
   - `S3Uploader.Upload` already accepts `io.Reader` — just stop wrapping an
     already-buffered slice.
5. Re-verify `StreamEncoder`/`ProgressiveStreamEncoder` still make sense once
   the upstream encode itself streams (today they only chunk HTTP writes of
   an already-complete buffer, so the real memory win requires collapsing
   encode+write into one pass).

## Alternatives Considered

### A. Migrate to vipsgen now — rejected
Was the only path when investigated in 2025; superseded once govips added
native streaming in the same dependency already used here. No reason to take
on a second libvips binding.

### B. Pin to the untagged govips commit now — rejected (for now)
Would unblock immediately, but the API isn't tagged/released and could still
change. Revisit if this becomes urgent (e.g. memory pressure in prod) before
a tag lands.

## Consequences

- No code changes yet. This ADR exists to record the finding so it isn't
  re-researched later — govips already has the fix, no vipsgen migration
  needed, just waiting on a tagged release.
- When revisited, the interface changes (`MediaProcessor`, `Encoder`,
  `ResponseOpts`) touch the same call sites across `src/handlers/*` that
  wire up `job.Encoder`.

## Follow-up trigger

Check `github.com/davidbyttow/govips` releases for a tag newer than
`v2.18.0` that includes the `LoadImageFromReader` / `SaveToWriter*` streaming
API (merged as PR #539, 2026-08-07, with a follow-up fix 2026-08-08). Once
tagged, promote this ADR to "Proposed" and schedule the migration.

---
**Risk Level:** Low (no action taken yet)
**Review Date:** revisit when govips ships a release containing streaming I/O
