# parabens.vc

Simple congratulations page with balloons, confetti, and basic analytics.
All static assets are embedded in the Go binary for single-file deployment.

## Features

- 🎉 Personalized congratulations pages at `/{name}`
- 🔗 Short link creation and management
- 📊 Privacy-focused analytics (logged to stdout)
- 🖼️ Dynamic OpenGraph images with custom text
- 🚫 Content filtering with blocked word list
- 🔒 Security headers and rate limiting

## Quick Start

### Build and Run

```bash
# Build using Makefile (creates arm64 and amd64 binaries)
make

# Or build for current platform
go build -o parabens-vc .

# Run the server
./parabens-vc
```

Visit `http://localhost:8080/SeuNome` to see a personalized page.

### Docker

```bash
# Using docker-compose
docker-compose up

# Or build and run manually
docker build -t parabens-vc .
docker run -p 8080:8080 parabens-vc
```

## Configuration

Environment variables:

- `PORT`: Server port (default: `8080`)
- `PUBLIC_BASE_URL`: Base URL for og:url and short links (default: `https://parabens.vc`)
- `SHORTLINK_DB`: Path to shortlinks storage file (default: `data/shortlinks.json`)

## API

### Short Links

**Create a short link:**
```bash
POST /s
Content-Type: application/json

{ "path": "Parabéns,_Renato!" }
```

Response:
```json
{
  "code": "abc1234",
  "short_url": "https://parabens.vc/s/abc1234",
  "path": "Parabéns,_Renato!",
  "destination": "https://parabens.vc/Parab%C3%A9ns,_Renato!"
}
```

**Resolve a short link:**
```
GET /s/{code}
```
Redirects to the original path.

**Rate limits:**
- Short link creation: 20 requests/minute per IP
- Analytics tracking: 120 requests/minute per IP

### Analytics

Track events by sending POST requests to `/api/track`. Events are logged to stdout with metadata (IP, user agent, referrer, language).

## Development

### Project Structure

- `main.go` - Main server implementation
- `main_test.go` - Test suite
- `public/` - Embedded static assets (HTML, CSS, JS, images)
- `.github/workflows/` - CI/CD pipelines for building binaries and Docker images

### Testing

```bash
go test -v ./...
```

## Deployment

GitHub Actions automatically builds:
- Multi-platform binaries (Linux arm64/amd64)
- Docker images pushed to GitHub Container Registry

Pull the latest image:
```bash
docker pull ghcr.io/renatolfc/parabens.vc:main
```

## Notes

- Paths are URL-decoded and underscores are converted to spaces
- Dynamic OG images require `rsvg-convert` (install `librsvg2-bin` on Debian/Ubuntu)
- Privacy policy available at `/privacy`
