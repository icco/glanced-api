# glanced-api

Sync API for [Glanced](https://github.com/icco/glanced), a digital take on Analog task cards. It stores Today, Next, and Someday cards in Postgres and syncs them with the Android app using last-write-wins push and cursor pull.

## Run

```bash
docker run -p 8080:8080 -e DATABASE_URL=postgres://... ghcr.io/icco/glanced-api:main
```

Create a user (there is no public sign-up). The password is read from stdin:

```bash
printf '%s\n' 'correct horse battery staple' | docker run -i -e DATABASE_URL=postgres://... ghcr.io/icco/glanced-api:main /app/bin/glanced-api user create --email ada@example.com
```

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | none | Postgres connection string |
| `PORT` | `8080` | HTTP listen port |

## API

- `POST /v1/auth/login` `{email, password}` returns `{token}`
- `POST /v1/auth/logout`
- `POST /v1/sync/push` `{device_id, cards, lines}` returns `{accepted}`
- `GET /v1/sync/pull?cursor=N&limit=M` returns `{cards, lines, cursor, more}`

Authenticated routes take `Authorization: Bearer <token>`. The design lives in [icco/glanced](https://github.com/icco/glanced/blob/main/docs/superpowers/specs/2026-11-12-analog-rewrite-design.md).

## Develop

```bash
task test
```

Tests start an embedded Postgres, so no database or Docker is needed.
