# parabens.vc

Simple congratulations page with balloons, confetti, and basic analytics.
All static assets are embedded in the Rust binary for single-file deployment.

## Run

1. Build and start the server:
   - `cargo run`
2. Open in a browser:
   - `http://localhost:8080/SeuNome`

To change the port, set `PORT` (e.g., `PORT=9999 cargo run`).
Set `PUBLIC_BASE_URL` to control the `og:url` (defaults to <https://parabens.vc>).

## Short links

- Create: `POST /s` with JSON `{ "path": "Parabéns,_Renato!" }`
- Resolve: `GET /s/{code}` redirects to `/{path}`
- Storage: set `SHORTLINK_DB` (defaults to `data/shortlinks.json`)
- Rate limit: 20 requests per minute per IP (in-memory)
- Caching: same `path` returns the same short URL

## Notes

- The path segment becomes the congratulated name.
- Query strings are supported and included in analytics payloads.
- Analytics events are sent to `/api/track` and logged to stdout (path, query, params, user agent, referrer, accept-language, timezone, screen, viewport).
- Privacy policy is available at `/privacy`.
