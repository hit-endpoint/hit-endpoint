# Installing Hit Endpoint (`hit`)

`hit` is distributed as a single standalone executable with zero external runtime dependencies. Choose the installation method that fits your environment:

- [Option A: One-Liner Quick Install (macOS & Linux)](#option-a-one-liner-quick-install-macos--linux)
- [Option B: Download Pre-Built Binaries](#option-b-download-pre-built-binaries)
- [Option C: Go Install (Go 1.22+)](#option-c-go-install-go-122)
- [Option D: Build from Source](#option-d-build-from-source)
- [Verifying Installation](#verifying-installation)

---

## Option A: One-Liner Quick Install (macOS & Linux)

The fastest method for macOS and Linux systems. Downloads the appropriate binary for your architecture, verifies checksums, performs ad-hoc codesigning (macOS), and places `hit` in `/usr/local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/hit-endpoint/hit-endpoint/main/install.sh | sh
```

---

## Option B: Download Pre-Built Binaries

Pre-compiled standalone release archives are published for all major OS platforms and architectures on the [**GitHub Releases**](https://github.com/hit-endpoint/hit-endpoint/releases/latest) page:

| Platform | Architecture | Binary Archive |
|---|---|---|
| **macOS** | Apple Silicon (`arm64`) | [`hit-darwin-arm64.tar.gz`](https://github.com/hit-endpoint/hit-endpoint/releases/latest) |
| **macOS** | Intel (`amd64`) | [`hit-darwin-amd64.tar.gz`](https://github.com/hit-endpoint/hit-endpoint/releases/latest) |
| **Linux** | x86_64 (`amd64`) | [`hit-linux-amd64.tar.gz`](https://github.com/hit-endpoint/hit-endpoint/releases/latest) |
| **Linux** | ARM64 (`arm64`) | [`hit-linux-arm64.tar.gz`](https://github.com/hit-endpoint/hit-endpoint/releases/latest) |
| **Windows** | x64 (`amd64`) | [`hit-windows-amd64.zip`](https://github.com/hit-endpoint/hit-endpoint/releases/latest) |

### Unpacking and Manual Installation

#### macOS (Apple Silicon / Intel)

```bash
# 1. Unpack the downloaded archive
tar -xzf hit-darwin-arm64.tar.gz   # use hit-darwin-amd64.tar.gz for Intel Macs

# 2. Clear browser quarantine attribute & apply ad-hoc signature (required on Apple Silicon)
xattr -d com.apple.quarantine hit 2>/dev/null || true
codesign -s - -f hit 2>/dev/null || true

# 3. Move binary into your system PATH
sudo mv hit /usr/local/bin/

# 4. Verify installation
hit --version
```

> **Why ad-hoc signing is needed on macOS**: When downloading archives through a web browser (Safari, Chrome), macOS attaches a `com.apple.quarantine` extended attribute that causes Gatekeeper to block unsigned binaries. Running `xattr -d com.apple.quarantine hit` removes this flag, and `codesign -s - -f hit` creates a valid local ad-hoc signature. The one-liner `install.sh` handles this automatically.

#### Linux (x86_64 / ARM64)

```bash
# 1. Unpack the downloaded archive
tar -xzf hit-linux-amd64.tar.gz    # use hit-linux-arm64.tar.gz for ARM64

# 2. Move binary into your system PATH
sudo mv hit /usr/local/bin/

# 3. Verify installation
hit --version
```

#### Windows (x64)

1. Download [`hit-windows-amd64.zip`](https://github.com/hit-endpoint/hit-endpoint/releases/latest).
2. Extract `hit.exe` into a folder of your choice (e.g. `C:\Tools\hit\` or `%USERPROFILE%\bin\`).
3. Add that directory to your `PATH` environment variable:
   - In PowerShell (Current User):
     ```powershell
     [Environment]::SetEnvironmentVariable("PATH", $env:PATH + ";$HOME\bin", "User")
     ```
4. Open a new terminal and verify:
   ```powershell
   hit --version
   ```

---

## Option C: Go Install (Go 1.22+)

If you have Go 1.22 or higher installed:

```bash
go install github.com/hit-endpoint/hit-endpoint/cmd/hit@latest
```

Make sure `$GOPATH/bin` (or `~/go/bin`) is in your system `PATH`:

```bash
export PATH="$HOME/go/bin:$PATH"
```

---

## Option D: Build from Source

To compile the latest binary directly from source:

```bash
# Clone the repository
git clone https://github.com/hit-endpoint/hit-endpoint.git
cd hit-endpoint

# Build binary
go build -o hit ./cmd/hit

# (Optional) Move to system PATH
sudo mv hit /usr/local/bin/
```

---

## Verifying Installation

Run `hit --version` to check your installed version:

```bash
hit --version
```

Check available commands and flags:

```bash
hit --help
```

> **Certificates Note**: HTTPS requests use your operating system's root certificate store. Pass `-k` or `--insecure` in any `hit` command to skip TLS certificate verification for local development or self-signed test environments.
