# ADR: Video Frame Handling — Spritesheets + On-demand Hybrid (detailed)

**Status:** Accepted
**Date:** 2025-09-12
**Authors:** (you) — ffmpeg/core-worker design owner

---

## Decision summary (one line)

Generate low-fps **spritesheets** at ingest (cheap, CDN-cached) + run **on-demand high-fps frame extraction** for editor/zoom requests (ECS/SQS), store long-term in S3 and short-term in EFS or ephemeral S3 with TTL — tradeoff: minimal storage growth, predictable costs, acceptable latency for editors.

---

## Context & requirements

* Users upload videos (avg **2 min, 1080p, 30fps**, \~30MB compressed).
* Features required: scrub previews, fine-grain zoom to millisecond precision (editor), ML frame extraction (clip models), potential sprite/VTT timelines.
* Constraints: avoid exploding S3 object counts and PUT costs, keep scrubbing latency low, allow editor workflows to access 24–30fps segments when needed.
* Infrastructure: AWS (S3, CloudFront, ECS or EKS, EFS), SQS for job routing, optional Lambda for lightweight tasks.
* Operational expectations: predictable monthly cost (10k/100k/1M videos scenarios), autoscaling, monitoring, secure access.

---

## Alternatives considered (brief)

1. **Store all frames** (pre-extract every frame to separate objects). — **Rejected**: massive S3 object count, huge PUT costs and S3 list/get load.
2. **Spritesheets only** (low-fps tiles only). — **Acceptable** for previews, **insufficient** for editor precision.
3. **On-demand only** (never pre-extract). — **Cheapest storage**, but high and unpredictable compute spikes and visible latency for scrubbing.
4. **Hybrid (chosen)** — sprites as baseline + on-demand extraction when higher fidelity required. Best ROI.

(Reasons summarized: A → cost & ops impractical; B → UX limitations; C → latency & scaling risk; D → balanced.)

---

## Decision details & components

### 1) Ingest pipeline (synchronous/async)

* Trigger: SNS event on S3 upload (video).
* Steps:

  1. Validate media type (mp4, webm, mov, avi).
  2. Persist source video to S3 `s3://{bucket}/videos/{video_id}/source.mp4`.
  3. Enqueue an SQS job `generate_sprites` → ECS worker picks up.
  4. Worker generates:

     * Low-fps frames (config default: **6 fps**) → pack into spritesheets (e.g. 30 frames per sheet).
     * Manifest JSON describing mapping: time ranges → sprite file & tile coordinates, plus `sprite_interval`, `frames_per_sprite`, `fps_baseline`.
  5. Upload sprites + manifest to S3; set appropriate metadata headers (e.g., `x-amz-meta-manifest` or a manifest object). CDN (CloudFront) invalidation not necessary on first upload.
  6. Emit completion event to indexer / metadata DB.

### 2) On-demand extraction (editor/zoom)

* Trigger: frontend requests high fidelity for a specific time window (API `/frames?video_id=&start=&duration=&fps=`).
* Flow:

  1. API checks cache (CloudFront or short-TTL S3 or EFS) for already extracted frames. If available, return.
  2. If not, enqueue an SQS job `extract_frames` with `start, duration, fps`.
  3. ECS worker pops job, extracts frames (ffmpeg), stores output in **EFS** (mounted) or S3 with short TTL (put to `s3://tmp/frames/{video_id}/{request_id}/...`) and updates cache.
  4. Return presigned URLs or stream sprites back to FE.
  5. Set a TTL cleanup policy to delete temp frames after X hours/days.

### 3) Storage locations & caching

* **Long-term**: S3 Standard for source videos and baseline spritesheets + manifest. Use CloudFront for public/fast retrieval. S3 lifecycle to transition older assets to IA/Glacier. S3 is cheapest & CDN friendly. (S3 pricing reference.) ([Amazon Web Services, Inc.][1])
* **Short-term / shared fast filesystem**: EFS for temporary high-fps frames when workers need POSIX shared access and low latency; alternatively use ephemeral container storage + push to S3 for persistence. EFS typical list price \~ \$0.30/GB-month (region dependent). ([Amazon Web Services, Inc.][2])
* **CDN**: CloudFront for sprites & temp caches — egress ≈ \$0.085/GB (US) after first TB; request pricing applies. ([Amazon Web Services, Inc.][3])

### 4) Worker Dockerfile & codec requirements (notes)

Must include ffmpeg built with these libs:

* Video: `libx264`, `libx265`, `libvpx`, `libaom`, `libsvtav1` (AV1)
* Audio: `libfdk_aac` / `aac` (as required)
* Image packing: `ImageMagick` or `libvips` (prefer libvips for performance)
* Tools: `awscli`, small Python/Go binary to read manifest templating and upload results
  Example base:

```
FROM ubuntu:22.04
RUN apt-get update && apt-get install -y ffmpeg imagemagick libvips awscli
# Or build ffmpeg from source with required libs
COPY worker /usr/local/bin/worker
ENTRYPOINT ["/usr/local/bin/worker"]
```

### 5) DSL (simple, explicit, matches your ask)

We keep the DSL intentionally tiny and command-first. This exact YAML block is used by the worker to render template variables and run shell commands.

```yaml
- action: generate_sprites
  defaults:
    fps: 6
    frames_per_sprite: 30
    out_format: jpg
  command: |
    ffmpeg -hide_banner -loglevel error -i {{input}} -vf fps={{fps}},scale={{scale}} {{workdir}}/frame_%05d.{{out_format}}
    # pack with libvips or ImageMagick
    vips arrayjoin {{workdir}}/frame_*.{{out_format}} {{output}} --across {{frames_per_sprite}}
  args:
    - input
    - output
    - fps
    - frames_per_sprite
    - scale

- action: extract_frames
  defaults:
    fps: 24
    duration: 1
  command: |
    ffmpeg -hide_banner -loglevel error -ss {{start}} -i {{input}} -t {{duration}} -vf fps={{fps}},scale={{scale}} {{outputdir}}/frame_%05d.jpg
  args:
    - input
    - start
    - duration
    - fps
    - outputdir
```

(Workers will substitute `{{...}}` using the job payload. No full DSL engine required — just templating + validation.)

### 6) ffmpeg commands (concrete)

* Baseline frame extraction (6fps):

```
ffmpeg -i source.mp4 -vf fps=6,scale=1280:-2 workdir/frame_%05d.jpg
```

* On-demand precise extraction (24fps, 1s window starting at 00:00:12.500):

```
ffmpeg -ss 00:00:12.500 -i source.mp4 -t 1 -vf fps=24,scale=1920:-2 tmp/frames/frame_%04d.jpg
```

* Sprite pack (ImageMagick):

```
montage tmp/frames/*.jpg -tile 6x5 -geometry +0+0 sprites/sprite_0001.jpg
```

* Sprite pack (libvips, faster, less memory):

```
vips arrayjoin tmp/frames/*.jpg sprites/sprite_0001.jpg --across 30
```

---

## Cost model (realistic projections)

Sources: AWS S3 pricing pages and third-party price summaries for 2025 (tiered S3 \$0.023/GB first 50TB), EFS guides (\~\$0.30/GB-mo), CloudFront egress \$0.085/GB (US). See citations. ([Amazon Web Services, Inc.][1])

**Assumptions (baseline):**

* Video: 2 min ≈ 30 MB stored in S3 (source).
* Baseline sprites: 6fps → 720 frames → packed into 24 sprites @ 150 KB per sprite → **3.6 MB per video** stored in S3.
* On-demand zoom: assume 1% of video views require high-fps extraction; each extraction produces 24 frames (24 × 30KB = 720 KB) and is kept cached for 24 hours on EFS/S3 (temp).
* PUT/GET request pricing: S3 PUT \~\$0.005/1k, GET \~\$0.0004/1k (approx; region var). ([Amazon Web Services, Inc.][1])
* EFS cost used: \$0.30/GB-month (multi-AZ standard). ([Cloudchipr][4])
* EC2/ECS compute: **estimate** for spot vCPU-hr \$0.02/vCPU-hr (region & instance type dependent). *This is an estimate to size compute cost — verify with current region spot prices.* (no single authoritative spot price page given; use internal finance/tracking).

> **Note:** exact AWS costs vary by region and usage patterns (requests, tiered storage). Use these as order-of-magnitude for decision making; finalize with actual invoices/region quotes.

### Per-month projections

#### 1) **10,000 videos/month ingest**

* S3 storage for sprites: `10k × 3.6 MB ≈ 36 GB` → **S3 cost** ≈ `36 GB × $0.023` ≈ **\$0.83/mo**. ([nOps][5])
* Source videos S3: `10k × 30 MB = 300 GB` → **\$6.9/mo**.
* Total S3 storage ≈ **336 GB** → **\$7.7/mo**.
* PUTs: sprites (24 per video) + source PUT → \~25 PUTs/video = 250k PUTs → at \$0.005/1k ≈ **\$1.25**. ([Amazon Web Services, Inc.][1])
* EFS temp storage if used for on-demand cache: assume 1% zooms → `100 zooms/day × 0.72 MB/temp ≈ 72 MB/day` stored ephemeral — negligible; assume EFS cost **<\$1/mo**. ([Cloudchipr][4])
* Compute (sprite generation): assume 0.2 vCPU-hr/video → `10k × 0.2 = 2k vCPU-hr` → at \$0.02/vCPU-hr = **\$40** (spot) — *estimate*.
* CloudFront egress: depends on viewer traffic; baseline previews are small (sprites). Assume **moderate** egress; not included here — estimate separately per traffic.

**10k total (monthly)** ≈ **\$50–60/mo** (storage + PUTs + compute), plus CloudFront egress.

#### 2) **100,000 videos/month ingest**

* S3 sprites: `100k × 3.6 MB = 360 GB` → **\$8.3/mo**.
* Source videos: `100k × 30 MB = 3 TB` → **\$69/mo**.
* Total S3 storage ≈ **3.36 TB** → **\$77/mo**.
* PUTs: 2.4M PUTs → \~\$12/mo.
* Compute (sprite gen): `100k × 0.2 = 20k vCPU-hr` → **\$400** (spot).
* EFS for temp frames: 1% zooms = `1000/day × 0.72MB ≈ 720MB/day` retained 1 day → \~22GB/month peak retention estimate → **\$6.6/mo**.
  **100k total** ≈ **\$500–600/mo** + egress.

#### 3) **1,000,000 videos/month ingest**

* S3 sprites: `1M × 3.6 MB = 3.6 TB` → **\$82.8/mo**.
* Source videos: `1M × 30 MB = 30 TB` → **\$690/mo** (first 50TB tier \$0.023/GB). ([nOps][5])
* Total S3 storage ≈ **33.6 TB** → **\$773/mo**.
* PUTs: 24M PUTs → **\$120/mo**.
* Compute (sprite gen): `1M × 0.2 = 200k vCPU-hr` → **\$4,000** (spot estimate).
* EFS temp for zoom cache: 1% zooms = `10k/day × 0.72MB ≈ 7.2GB/day` retained → \~216GB/month → **\$64.8/mo** (EFS). ([Cloudchipr][4])

**1M total** ≈ **\$5k–6k/mo** (storage + compute + temp EFS + PUTs), plus egress.

> **Interpretation:** S3 storage & requests remain a small fraction with spritesheets. The largest variable is compute if on-demand extraction volume rises (editor heavy load). EFS cost only matters if you keep many extracted frames cached long time; keep TTL aggressive.

---

## Why EFS vs S3 for temp frames

* **Use EFS when**:

  * Multiple ECS tasks need POSIX shared access.
  * Low latency file IO is required for post-processing (montage, libvips).
  * You expect lots of short-lived files that are heavily IO bound.
* **Use S3/ephemeral S3 for temp frames when**:

  * You prefer object durability and CDN integration.
  * Workers can write to S3 directly and send presigned URLs.
  * Avoid EFS steady-state cost when temp data volume low.

**Hybrid recommendation**: write extracted frames to EFS for immediate worker operations, then upload a compressed package (sprite/tar) to S3 for caching and serve via CloudFront. Delete EFS files per TTL.

---

## Operational considerations

### Scalability & autoscaling

* Autoscale ECS worker group based on **SQS queue depth** (primary) and **CPU utilization** (secondary).
* Use Spot instances for workers to reduce cost; set a fallback to on-demand for critical steady work. ([Amazon Web Services, Inc.][6])

### Caching & CDN

* CloudFront in front of S3 for sprites and extracted frames (short TTL for temp frames). Optimize cache control headers: sprites → long TTL (365d), manifest → moderate TTL (1h). ([Amazon Web Services, Inc.][3])

### Lifecycle & cleanup

* S3 lifecycle rules:

  * Sprites: Standard for 30–90 days, then IA or Glacier for older than X months.
  * Temp frames bucket: Transition to Glacier or delete after 24–72 hours.
* EFS cleanup: background process or TTL + lifecycle manager to purge old temp directories.

### Monitoring & SLOs

* Metrics to collect:

  * Ingest latency (upload → sprites ready).
  * Average sprite generation time (vCPU-s).
  * On-demand extraction latency (queue wait, extraction time).
  * Number of temporary files and EFS utilization.
  * Cost per video (weekly rolling).
* SLO examples:

  * Baseline sprites available within **30s** of ingest for >99%.
  * On-demand extraction for 1s window at 24fps within **500ms–1.5s** (depends on cold worker).
* Alerting: queue growth > threshold, EFS near 80% capacity, worker error rates > 2%.

### Security

* S3 objects set to private; access via presigned URLs or CloudFront with signed URLs.
* Workers assume IAM role with least privileges (S3 put/get limited to the bucket prefix, EFS access via network + security group).
* Scan uploads for malware (optional) if user-uploaded content is untrusted.

---

## Risks & mitigations

* **High editor load → compute explosion**: Mitigate with rate limits, priority queues, worker autoscale caps and caching. Consider prewarming pool for high concurrency.
* **S3 Object count & PUT costs if approach drifts to storing many temp objects**: Use packed sprites and tar archives to reduce object counts, and lifecycle rules to auto-delete temp objects.
* **EFS cost blowup**: enforce TTLs and metrics; prefer ephemeral container FS + S3 when feasible.

---

## Implementation plan (concrete next steps)

1. Finalize worker Dockerfile (ffmpeg + libvips + awscli).
2. Implement minimal DSL parser to apply templates to two actions: `generate_sprites` and `extract_frames`. Keep it tiny.
3. Build ECS worker with SQS consumer and instrumented metrics (Prometheus/CloudWatch).
4. Deploy S3 buckets and CloudFront distribution with cache policy for sprites.
5. Create lifecycle rules for temp buckets and EFS cleanup.
6. Run load test: simulate 10k/100k ingest + 1% zoom load and measure vCPU-hr and latencies; iterate defaults (fps, frames\_per\_sprite).
7. Optimize cost/location (region spot prices, S3 tiering).

---

## Appendix — quick reference snippets

**DSL sample**

```yaml
- action: generate_sprites
  defaults:
    fps: 6
    frames_per_sprite: 30
  command: |
    ffmpeg -i {{input}} -vf fps={{fps}},scale={{scale}} {{workdir}}/frame_%05d.jpg
    vips arrayjoin {{workdir}}/frame_*.jpg {{output}} --across {{frames_per_sprite}}
```

**ffmpeg sample (24fps, 1s)**

```
ffmpeg -ss 00:00:12.500 -i s3://bucket/videos/vid.mp4 -t 1 -vf fps=24,scale=1280:-2 /tmp/out/frame_%04d.jpg
```

---

## Sources & citations

* AWS S3 pricing (standard, requests) — AWS docs. ([Amazon Web Services, Inc.][1])
* S3 tier & per-GB numbers (2025 summaries & tiering: \$0.023/GB first 50 TB). ([nOps][5])
* Amazon EFS pricing summary (\~\$0.30/GB-month). ([Amazon Web Services, Inc.][2])
* CloudFront data transfer and request pricing (example \$0.085/GB US). ([Amazon Web Services, Inc.][3])
* EC2 & Spot pricing reference pages (spot behavior; use region pricing for exact numbers). ([Amazon Web Services, Inc.][7])

---

If you want, I’ll:

* produce a **one-page runbook** (commands to triage slow extractions, reclaim EFS space, and tune autoscale), and
* produce **the exact worker Dockerfile + CI build script + templated systemd/ECS task definition** ready for your pipeline. Which do you want first?

[1]: https://aws.amazon.com/s3/pricing/ "S3 Pricing"
[2]: https://aws.amazon.com/efs/pricing/ "Amazon EFS Pricing"
[3]: https://aws.amazon.com/cloudfront/pricing/ "Amazon CloudFront CDN - Plans & Pricing - Try For Free"
[4]: https://cloudchipr.com/blog/aws-efs "The Complete Guide to Amazon EFS: Features, Pricing ..."
[5]: https://www.nops.io/blog/how-much-do-aws-s3-storage-classes-cost/ "The Ultimate Guide to AWS S3 Storage Cost 2025 - nOps"
[6]: https://aws.amazon.com/ec2/spot/pricing/ "Amazon EC2 Spot Instances Pricing"
[7]: https://aws.amazon.com/ec2/pricing/on-demand/?utm_source=chatgpt.com "EC2 On-Demand Instance Pricing - AWS"
