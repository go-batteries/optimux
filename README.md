# optimux

A self-hosted, on-the-fly image and video transformation server: a
self-hosted alternative to imgproxy or thumbor, but with video support
(sprites, transcoding) built in, not bolted on. Point it at your S3
bucket, request a resize/format/quality by URL, and it fetches,
transforms, and serves the result, caching what it can along the way.

`go get github.com/go-batteries/optimux`

Licensed under [AGPL-3.0](./LICENSE).

## Why

Traditional DAM (digital asset management) setups pre-generate every
size/format combination a listing site might need, ahead of time. That
means storage cost and processing time up front for combinations that
may never get requested. optimux generates them on demand instead, and
caches at three tiers (Nginx edge -> tmpfs -> S3) so the second request
for the same size is fast without ever pre-computing it.

## Real numbers

Measured directly against `cmd/server` (the actual production binary,
not a synthetic benchmark). Two separate data sets, both real, neither
should be read as "the" official number - see caveats below each.

**Sustained load** (`wrk`, single real ~1MB source image, resized to
400x600 + re-encoded as WebP, run on a local machine, not the AWS
deployment):

| Concurrency | Req/s | Avg latency |
|---|---|---|
| 10 | 32.6 | 303ms |
| 50 | 44.2 | 1.05s |

*Caveats: run on a laptop (Apple M1), not the production EC2 instance -
useful for relative before/after comparisons, not as an absolute
production throughput claim. Also hits the same source image
repeatedly, so the network fetch is cache-warm after the first request
(source-image tmpfs cache) - the resize/encode work itself is NOT
cached or skipped (that path depends on an S3 object-metadata check
that only applies to real S3-backed deployments), so every request in
this table did full, real libvips work. Per `Progression.md`'s own
timing breakdown, processing costs ~7-10x more than fetching, so this
should be close to a mixed-image number, just slightly optimistic on
the fetch side.*

**Correctness under burst**: 48 concurrent requests across 6 different
real images, mixed sizes - all 48 succeeded, 0 rejected, ~1s wall time.
The dynamic worker scaler (2-10 workers, scales on queue depth)
absorbed the burst without dropping anything.

**Compression**: a 613KB JPEG resized to 400x600 and re-encoded as
WebP: 74KB, an 87.9% size reduction, ~63ms cold.

**From earlier development** (see `Progression.md` for the full log):
early iterations before the current worker-scaling architecture and
before caching was enabled measured 2-3.3 req/s single-threaded against
large (8K source) images on a small EC2 instance, with CPU steal up to
8.7% observed (`iostat`) - a reminder that on shared/burstable EC2
instance types, virtualization contention can dominate your latency
budget before your own code does. Those numbers predate the dynamic
scaler and streaming encoder and aren't representative of current
performance; they're kept here as a real trace of what got fixed.

## API

Base URL: `https://hostname/optimux/assets/<path-under-your-s3-bucket>`

Query params:
- `format=`: `webp` or `jpeg`
- `sizes=`: `WIDTHxHEIGHT`, comma-separated for multiple sizes in one request
- `quality=`: 1-90
- `encoder=`: `stream` (default), `json` (base64-encoded in a JSON body), or
  `progress` (chunked, aimed at HTTP/2 progressive loading)
- `thumb=true`: use the thumbnail loading strategy instead of full-size

Example: `https://hostname/optimux/assets/products/shoe.jpg?format=webp&sizes=400x600,800x0`

## Running

Only on Linux (tmpfs edge caching relies on it).

```shell
$ make tmpfs.setup
$ make gen.certs
$ make run.dynsc
```

To serve via nginx:

```shell
sudo cp ./nginx.optimux.conf /etc/nginx/sites-enabled
sudo mkdir -p /tmp/shm/edge_cache && sudo chown -R www-data:www-data /tmp/shm/edge_cache
sudo nginx -t
sudo systemctl reload nginx
```

If you want to add a new ENV variable.
- Add it to `Dockerfile`
- Add it to `./infra/supervisord.conf`
- Add it to `./infra/terraform/variables.tf`
- Pass it to the ec2 instance, by changing in `aws_ecs_task_definition` block in `main.tf`
- and `./infra/terraform/ecs-task-definition.json.tmpl`, under `environments`
