# Community QR Code Customization

This branch adds user-facing community QR codes and an admin editor while keeping the customization isolated from upstream Sub2API.

## Routes

- User page: `/community`
- Admin page: `/admin/community-qrcodes`
- User API: `GET /api/v1/community-qrcodes`
- Admin API: `GET|PUT /api/v1/admin/community-qrcodes`

The configuration is stored under the `community_qr_codes` setting key. Images are restricted to PNG, JPEG, or WebP Data URLs with a decoded size of at most 300 KB.

## Playground and image billing

The Playground shares the normal authenticated gateway, audit, usage logging, and balance billing. Chat and images use one conversation layout. Text history is account-scoped in Redis, expires seven days after creation, and supports optimistic revision checks across devices. Enable durable Redis persistence and retain its data volume. Each user can keep 30 conversations, with up to 200 messages and 256 KiB per conversation. Image previews currently remain in page memory for ten minutes and can be downloaded; leaving or refreshing clears them sooner.

Successful image generation is always billed per image, even if the channel or group has token pricing:

`user charge = per-image price × image count × group image_rate_multiplier`

The image multiplier applies even when the legacy `image_rate_independent` flag is false. Zero deliberately means free images. Text discounts and peak-hour text multipliers do not apply. Batch jobs also use this multiplier, retaining their existing batch discounts and reservation rules. Per-image price precedence remains group model image/request price, group size price, channel image/request price, then model defaults. Configure the 1K/2K/4K prices and multiplier before activation.

Account cost statistics retain their separate upstream pricing rules. A selling price below cost can still produce account costs above user charges. Historical usage is not recalculated; the new behavior takes effect after activating the updated service. Local regression checks include token-channel overrides, image counts, zero/fractional multipliers, text discount and peak-rate isolation, group pricing, batch jobs, and persisted charges.

## Playground attachments and downloadable files

The standard conversation accepts UTF-8 text/code files as reference context
and PNG/JPEG/WebP images as multimodal inputs. Binary office documents and PDFs
are not parsed: export their relevant text first. Text references travel inside
the ordinary user message, so existing group content review and usage
attribution apply. Image understanding depends on the selected model.
Reference image bytes are request-local, not persisted in seven-day history;
reselect images after refreshing or switching devices.

With image mode explicitly selected, reference images use the authenticated
`/api/v1/playground/images/edits` endpoint, reusing the existing image gateway,
content audit, balance holds, per-image pricing, and usage records. Unsupported
platforms return an error instead of silently generating without the reference.

Assistant text files use fenced blocks with `artifact:filename.ext` as the
language label. Download buttons produce local files from the returned content;
they do not execute code, fetch arbitrary URLs, or imply an upstream sandbox.
These textual artifacts inherit the conversation's seven-day retention.

## Synchronizing upstream

Use the `sub2api-sync-upstream` skill for publication: preserve published custom
history with a merge, validate locally, push the custom branch, and verify CI,
Security Scan, and Custom image for the exact commit. Production activation
requires separate authorization. The legacy helper below rebases and must not
be used for that merge-based publication workflow.

Run from a clean `custom/community-qrcode` branch:

```bash
./tools/sync-official.sh
```

The script fetches `upstream/main`, creates a rollback tag, rebases the customization, runs focused verification, and builds a versioned `linux/amd64` image when Docker is available. It never restarts production.

Set `GO_BIN` or `PNPM_BIN` when those tools are not on `PATH`. Pushing the synchronized branch to the fork automatically builds `ghcr.io/<fork-owner>/sub2api:community-latest` and an immutable `community-<commit>` tag. The workflow only publishes an image; it never connects to or restarts production.
