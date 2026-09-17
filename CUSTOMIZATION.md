# Community QR Code Customization

This branch adds user-facing community QR codes and an admin editor while keeping the customization isolated from upstream Sub2API.

## Routes

- User page: `/community`
- Admin page: `/admin/community-qrcodes`
- User API: `GET /api/v1/community-qrcodes`
- Admin API: `GET|PUT /api/v1/admin/community-qrcodes`

The configuration is stored under the `community_qr_codes` setting key. Images are restricted to PNG, JPEG, or WebP Data URLs with a decoded size of at most 300 KB.

## Synchronizing upstream

Run from a clean `custom/community-qrcode` branch:

```bash
./tools/sync-official.sh
```

The script fetches `upstream/main`, creates a rollback tag, rebases the customization, runs focused verification, and builds a versioned `linux/amd64` image when Docker is available. It never restarts production.

Set `GO_BIN` or `PNPM_BIN` when those tools are not on `PATH`. Pushing the synchronized branch to the fork automatically builds `ghcr.io/<fork-owner>/sub2api:community-latest` and an immutable `community-<commit>` tag. The workflow only publishes an image; it never connects to or restarts production.
