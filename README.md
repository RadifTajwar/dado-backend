# DADO — API (Go)

The backend for the DADO website and admin: services, media, portfolio, contact and trial
forms, clients and jobs, admin login (JWT, 24 hours), emails (Gmail) and image uploads
(Cloudinary). Data lives in MongoDB Atlas.

The frontend ([dado-frontend](https://github.com/RadifTajwar/dado-frontend)) is the only thing that talks to this API: the
browser calls `/api/*` on the website and Next.js forwards it here.

## Run locally

Needs Go 1.26+.

```bash
cp .env.example .env          # then fill in the values
go run ./cmd/api              # http://localhost:8080  (add -seed-demo once for sample data)
go test ./...
```

On first start it creates the admin from `ADMIN_EMAIL` / `ADMIN_PASSWORD` and fills the
services, page images and before/after pairs. Later starts leave existing data alone.

## Where things are

| Path                         | What it does                                          |
| ---------------------------- | ----------------------------------------------------- |
| `cmd/api/main.go`            | Starts the server                                     |
| `internal/routes/routes.go`  | **Every API route, in one file**                      |
| `internal/handlers/`         | One file per resource + `validate.go` (form rules)    |
| `internal/models/`           | Data shapes (JSON matches the frontend's `lib/types.ts`) |
| `internal/auth/`             | Passwords, JWT cookie, rate limits                    |
| `internal/mail/`             | Gmail sending + email templates (`templates/`)        |
| `internal/seed/`             | First-run data and `-seed-demo` sample data           |
| `internal/config/`, `internal/db/` | Settings from the environment, MongoDB connection |

## Settings

Every setting is an environment variable; see `.env.example` for the full list with notes.
Locally they come from `.env`; on a host, set them in its dashboard.

| Variable | Notes |
| --- | --- |
| `PORT` | Set automatically by most hosts |
| `SITE_URL` | The website's public URL (used in email links) |
| `COOKIE_SECURE` | `true` in production (HTTPS) |
| `MONGODB_URI`, `MONGODB_DB` | Atlas connection string and database name (`dado`) |
| `JWT_SECRET` | 64 random hex characters; changing it signs everyone out |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD` | Only used to create the first admin |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `MAIL_FROM_NAME` | Gmail account that sends the mail + its app password |
| `NOTIFY_EMAIL` | Inbox that gets new contact / trial alerts |
| `CLOUDINARY_CLOUD_NAME`, `CLOUDINARY_API_KEY`, `CLOUDINARY_API_SECRET` | Image uploads |

## Deploy (Render)

The API is a long-running server (it sends email in the background and keeps rate limits in
memory), so it goes on a server host rather than Vercel. Render works like Vercel: connect the
GitHub repo and it redeploys on every push.

1. render.com → **New → Web Service** → connect this repository.
2. Runtime **Go**, Build command `go build -o bin/api ./cmd/api`, Start command `./bin/api`.
3. Instance type **Starter** or above. The free tier sleeps when idle, and the first request after
   that can take close to a minute, long enough for a form submission to time out.
4. Add every variable from `.env.example` (not `PORT`), with `COOKIE_SECURE=true` and
   `SITE_URL` = the website's URL.
5. Health check path: `/api/health`.
6. MongoDB Atlas → **Network Access** → allow the host (Render's outbound IPs, or
   `0.0.0.0/0` together with a strong database password).

Copy the service URL (for example `https://dado-api.onrender.com`) into the frontend's
`BACKEND_URL` setting on Vercel.

## Good to know

- Rate limits (login, forms, password reset) are per visitor only when the request arrives
  through the website: Vercel sets `X-Forwarded-For` to the visitor's real IP. Don't publish this
  API's own URL anywhere.
- Images are only accepted from our Cloudinary account and Unsplash. The cloud name is
  mirrored in `internal/handlers/validate.go` and the frontend's `next.config.ts` and
  `lib/admin/images.ts`; change all three together.
