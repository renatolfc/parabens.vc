# parabens.vc

Simple congratulations page with balloons, confetti, and basic analytics.
All static assets are embedded in the Go binary for single-file deployment.

## Features

- 🎉 Personalized congratulations pages at `/{message}`
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

Visit `http://localhost:8080/você_é_demais!` to see a personalized congratulations page.

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

### Setup

Enable the pre-commit hook to auto-format code and remove trailing whitespace:

```bash
ln -sf ../../scripts/pre-commit .git/hooks/pre-commit
```

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

## systemd (Arch)

1) Create user and directories:

```bash
sudo useradd --system --home /opt/parabens.vc --shell /usr/bin/nologin parabens
sudo install -d -o parabens -g parabens /opt/parabens.vc
sudo install -d -o parabens -g parabens /var/cache/parabens.vc
```

1) Install the binary:

```bash
sudo install -m 0755 ./parabens-vc /usr/local/bin/parabens-vc
```

1) Create environment file at /etc/parabens-vc.env:

```bash
PORT=8080
PUBLIC_BASE_URL=https://parabens.vc
SHORTLINK_DB=/opt/parabens.vc/data/shortlinks.json
XDG_CACHE_DIR=/var/cache
```

1) Install the service file:

```bash
sudo install -m 0644 deploy/parabens-vc.service /etc/systemd/system/parabens-vc.service
sudo systemctl daemon-reload
sudo systemctl enable --now parabens-vc
```

### Log persistence (journald)

Enable persistent journald storage (Arch defaults to volatile):

```bash
sudo mkdir -p /var/log/journal
sudo systemctl restart systemd-journald
```

View logs:

```bash
journalctl -u parabens-vc -f
```

## systemd --user (start at login)

1) Prepare directories in your home folder:

```bash
install -d ~/parabens.vc ~/parabens.vc/data ~/.cache/parabens.vc
```

1) Place the binary in your home folder:

```bash
install -m 0755 ./parabens-vc ~/parabens.vc/parabens-vc
```

1) Create env file at ~/parabens.vc/.env:

```bash
PORT=8080
PUBLIC_BASE_URL=https://parabens.vc
SHORTLINK_DB=%h/parabens.vc/data/shortlinks.json
XDG_CACHE_DIR=%h/.cache
```

1) Install and enable the user service:

```bash
install -m 0644 deploy/parabens-vc.user.service ~/.config/systemd/user/parabens-vc.service
systemctl --user daemon-reload
systemctl --user enable --now parabens-vc
```

Optional: keep the service running after logout:

```bash
loginctl enable-linger $USER
```

Logs:

```bash
journalctl --user -u parabens-vc -f
```

## Notes

- Paths are URL-decoded and underscores are converted to spaces
- Dynamic OG images require `rsvg-convert` and fonts. On Arch:
  - `pacman -S --needed librsvg ttf-opensans noto-fonts-emoji`
- Privacy policy available at `/privacy`
