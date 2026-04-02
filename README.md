# Anchor

A homelab-friendly configuration management system. One binary, plain shell scripts, no YAML DSL.

## Why Anchor?

Tools like Ansible, Chef, and Puppet are powerful but carry significant operational overhead: package dependencies, DSLs to learn, external databases, complex agent runtimes. If you manage a handful of Linux machines and want idempotent configuration with visibility into what's happening, Anchor is the simpler alternative.

- **Single binary** -- server, agent, key management, and signing all in one. No runtime dependencies.
- **Modules are shell scripts** -- no DSL. If you can write a shell script, you can write a module.
- **Embedded SQLite** -- no external database to provision or maintain.
- **Web UI included** -- dark mode, agent status, module assignment. No build step, no CDN.
- **Optional module signing** -- ed25519 signatures with SSH key support, so you can verify modules came from a trusted source.

## How it works

The **server** reads modules from a directory, serves a web UI, and responds to agent check-ins. The **agent** polls the server, downloads assigned modules and their artifacts, runs them, and reports results back.

```
[ Server ] <--- check in / report ---> [ Agent 1 ]
  modules.d/                            [ Agent 2 ]
  SQLite DB                             [ Agent 3 ]
  Web UI                                   ...
```

## Quick start

### Build

Requires Go 1.26+. No CGO needed.

```sh
go build ./cmd/anchor
```

Or use the Makefile:

```sh
make build    # build for current platform
make ci       # full check suite (fmt, vet, build, test)
```

### Run the server

```sh
anchor server \
  -port 8080 \
  -modules-dir /etc/anchor-server/modules.d \
  -data-dir /var/lib/anchor-server
```

### Run an agent

```sh
anchor agent \
  -server http://your-server:8080 \
  -data-dir /var/lib/anchor
```

Send `SIGHUP` to trigger an immediate check-in cycle. Systemd unit files with thorough sandboxing are provided in `contrib/`.

## Modules

A module is an executable script that accepts a single command argument:

```sh
#!/bin/sh
case "$1" in
    metadata)
        echo '{"name": "Hostname Check", "description": "Reports the current hostname"}'
        ;;
    apply)
        echo "hostname: $(hostname)"
        echo "fqdn: $(hostname -f 2>/dev/null || echo unknown)"
        exit 0   # 0 = ok, 80 = changed, anything else = error
        ;;
esac
```

The **filename** (e.g. `00_hostname`) is the canonical identifier -- modules execute in filename-sorted order. See `example/modules.d/` for more examples.

### Artifacts

A module can have an associated `<filename>.d/` directory containing supporting files (config templates, binaries, etc.). These are distributed to agents via content-addressable caching with preserved file permissions. Scripts access them as relative paths in their working directory.

### Signing

Modules can optionally be signed with ed25519 keys:

```sh
# Generate a keypair
anchor keygen -o mykey

# Sign modules
anchor sign -k mykey.key modules.d/00_hostname

# Run an agent that verifies signatures
anchor agent -server http://server:8080 \
  --verify-key ~/.ssh/id_ed25519.pub \
  --verify-key-url https://github.com/youruser.keys
```

Supports Anchor PEM format, OpenSSH keys, and `authorized_keys`-format URLs (e.g. GitHub's `.keys` endpoint). URL-fetched keys are cached for offline fallback.

## Status

Anchor is in early, active development by a single author. It is not yet versioned or tagged for release. The core feature set (server, agent, web UI, artifacts, signing) is functional, but the project should be considered pre-release software.

## License

See [LICENSE](LICENSE).
