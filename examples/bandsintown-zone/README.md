# Bandsintown Example Zone

An example **Hit Endpoint** zone demonstrating API testing, scenario chains, shorthands, and contract testing against the free, public [Bandsintown API](https://app.swaggerhub.com/apis/Bandsintown/PublicAPI/3.0.0).

## Overview

The Bandsintown API offers read-only access to artist profiles and events/tour dates:
- **Artist Info**: `GET /artists/{artistname}`
- **Artist Events**: `GET /artists/{artistname}/events` (supports `upcoming`, `past`, `all`, or date range `YYYY-MM-DD,YYYY-MM-DD`)

This zone includes a complete OpenAPI specification (`openapi.yaml`) so you can run offline tests immediately with Hit Endpoint's built-in dynamic mock server, or against the live Bandsintown API with an `app_id`.

## Quick Start (Offline Mock Server)

1. **Start the mock server in a terminal:**
   ```bash
   hit mock 8765 --openapi openapi.yaml
   ```

2. **Run all requests in the zone:**
   ```bash
   hit run bandsintown
   ```

3. **Run a single request:**
   ```bash
   hit run bandsintown/artists/01-get-artist
   ```

4. **Run multi-step chains:**
   ```bash
   hit run chains/artist-tour
   hit run chains/smoke
   ```

5. **Use shorthands:**
   ```bash
   hit artist
   hit events
   hit tour
   ```

6. **Validate zone and verify contract coverage:**
   ```bash
   hit validate
   hit report coverage --openapi openapi.yaml
   ```

## Servers

- **`local` (default)**: Targets the local mock server (`http://127.0.0.1:8765`).
- **`production`**: Targets `https://rest.bandsintown.com`.

To run against production with your custom application ID:
```bash
# Via environment variable:
BANDSINTOWN_APP_ID="your_app_id" hit -s production run bandsintown

# Or copy and customize the secrets file:
cp servers/production.secrets.example.yaml servers/production.secrets.yaml
# Edit servers/production.secrets.yaml, then run:
hit -s production run bandsintown
```

## Directory Structure

```text
examples/bandsintown-zone/
├── zone.yaml                     # Zone configuration, defaults, and shorthands
├── openapi.yaml                  # Bandsintown Public API 3.0.0 OpenAPI spec
├── shorthands.yaml               # Named endpoint presets (hit artist, hit events)
├── servers/
│   ├── local.yaml                # Mock server config (http://127.0.0.1:8765)
│   ├── production.yaml           # Live Bandsintown API config
│   └── production.secrets.example.yaml
├── collections/
│   └── bandsintown/
│       ├── _defaults.yaml        # Collection defaults (headers, app_id query)
│       ├── artists/
│       │   └── 01-get-artist.yaml
│       └── events/
│           ├── 01-upcoming-events.yaml
│           ├── 02-all-events.yaml
│           ├── 03-past-events.yaml
│           └── 04-date-range.yaml
└── chains/
    ├── artist-tour.yaml          # Multi-step scenario chain
    └── smoke.yaml                # Quick smoke test chain
```
