# loradex CLI

**Git for LoRAs, from the command line.** `loradex` lets you discover, pull, train, convert,
version, and publish LoRA models. It's built for pipelines and CI — every read command speaks
`--json`, every write command takes `-y/--yes`, and file bytes transfer **directly to/from
storage** via presigned URLs (never through the API).

```bash
loradex search "watercolor portrait" --base flux2-klein
loradex pull keenan/flux2-klein-portrait -o ./models/
loradex init my-lora && loradex build ./photos --base flux2-klein --trigger ohwxman
loradex convert ./lora.safetensors --to mlx
loradex push
```

- [Install](#install)
- [Concepts](#concepts)
- [Global flags](#global-flags)
- [Command reference](#command-reference)
- [Configuration & environment](#configuration--environment)
- [Exit codes](#exit-codes)
- [Security model](#security-model)

---

## Install

### Homebrew (recommended)

```bash
brew tap keeandrews/loradex
brew trust keeandrews/loradex     # Homebrew 6+ requires trusting third-party taps
brew install loradex
```

Upgrade later with `brew update && brew upgrade loradex`.

### One-line installer (portable, single-folder)

```bash
curl -fsSL https://raw.githubusercontent.com/keeandrews/loradex-cli/main/install.sh | sh
# custom home:  ... | sh -s -- --home ~/loradex
```

Everything loradex owns lives under **one home folder** (`$LORADEX_HOME`, default `~/.loradex`):
the binary (`bin/`), config, credentials, downloaded base models (`models/`), projects
(`projects/`), and trainer backends (`trainers/`). The installer builds the binary, runs the
setup wizard, and offers to add loradex to your PATH.

### From source (Go 1.26+)

```bash
go install github.com/keeandrews/loradex-cli/cmd/loradex@latest   # installs `loradex`
# or, in a checkout:
make build && ./loradex version
```

Cross-compiled for darwin/arm64, darwin/amd64, linux/amd64, linux/arm64, windows/amd64
(`make cross` → `dist/`).

---

## Concepts

- **Home folder** — a single portable directory (`$LORADEX_HOME`, default `~/.loradex`) holds
  config, credentials, downloaded base models, projects, and trainer backends.
- **Projects** — a managed workspace under `~/.loradex/projects/<name>/`. `loradex init` creates
  one and makes it *active*, so other commands work from any directory. A `.loradex/` workspace
  in the current directory still takes precedence.
- **Versions** — every published version is content-hash keyed and immutable; `latest` is an alias.
- **Formats** — `safetensors`, `mlx`, `diffusers`, `drawthings`. `loradex convert` moves between them.
- **Trainers** — `loradex build` orchestrates [ai-toolkit](https://github.com/ostris/ai-toolkit)
  as a subprocess; `loradex setup` detects/installs backends.

---

## Global flags

These apply to every command:

| Flag | Env | Description |
| --- | --- | --- |
| `--endpoint <url>` | `LORADEX_ENDPOINT` | API base URL (default `https://api.loradex.ai`). |
| `--web <url>` | `LORADEX_WEB` | Web base URL used for the login browser flow. |
| `--token <tok>` | `LORADEX_TOKEN` | Auth token override for CI. Never persisted to disk. |
| `--profile <name>` | `LORADEX_PROFILE` | Named credential/config profile (keep work + personal separate). |
| `--json` | | Machine-readable JSON output (read commands). |
| `-y, --yes` | | Assume yes; skip confirmation prompts (write commands). |
| `-q, --quiet` | | Suppress progress and non-essential output. |
| `-v, --verbose` | | Debug logging to stderr (secrets redacted). |
| `--no-color` | `NO_COLOR` | Disable ANSI color. |
| `--insecure` | | Allow `http://` for loopback endpoints only (never disables TLS verification). |
| `--models` | | Shortcut: open the base-model browser (same as `loradex models`). |

---

## Command reference

### Authentication

#### `loradex login`

Authenticate and store credentials for the targeted endpoint. Credentials are keyed by
endpoint, so a token is never sent to a host it wasn't issued for.

| Flag | Description |
| --- | --- |
| `--with-browser` | Use the browser OAuth flow (loopback `127.0.0.1` + PKCE). Default `true`; pass `--with-browser=false` to paste a token instead. |

```bash
loradex login                                 # browser flow
loradex login --with-browser=false            # paste a CLI token
LORADEX_TOKEN=lor9_… loradex whoami            # CI: token via env, never stored
loradex login --profile work                  # a separate credential profile
```

#### `loradex logout`

Remove stored credentials for the targeted endpoint/profile.

| Flag | Description |
| --- | --- |
| `--all` | Remove every stored credential across all endpoints and profiles. |

```bash
loradex logout
loradex logout --all
```

#### `loradex whoami`

Show the authenticated identity, plan, and storage usage. No flags.

```bash
loradex whoami
loradex whoami --json
```

---

### Discovery

#### `loradex search [query]`

Compatibility-first + full-text discovery of public repos.

| Flag | Description |
| --- | --- |
| `--base <id>` | Filter by base model. Repeatable (`--base flux2-klein --base sdxl`). |
| `--format <fmt>` | Filter by format (`safetensors`/`mlx`/…). Repeatable. |
| `--tag <tag>` | Filter by tag. Repeatable. |
| `--sort <key>` | `downloads` (default), `stars`, `updated`, or `new`. |
| `--limit <n>` | Max results, 1–100 (default 30). |
| `--page <n>` | Page number (default 1). |

```bash
loradex search "anime portrait"
loradex search --base flux2-klein --format safetensors "photoreal"
loradex search --tag portrait --tag photoreal --sort stars
loradex search "" --base sdxl --limit 100 --json
```

#### `loradex list`

List your repositories (or another owner's public ones).

| Flag | Description |
| --- | --- |
| `--owner <handle>` | List a specific owner's public repos instead of your own. |
| `--starred` | List the repos you've starred. |
| `--visibility <v>` | `public`, `private`, or `all` (default `all`). |
| `--sort <key>` | `downloads`, `stars`, `updated`, or `new`. |
| `--limit <n>` | Max results, 1–100 (default 30). |
| `--page <n>` | Page number (default 1). |

```bash
loradex list
loradex list --visibility private
loradex list --starred
loradex list --owner keenan --sort updated
```

#### `loradex view <owner>/<repo>`

Show a repository page in the terminal.

| Flag | Description |
| --- | --- |
| `--version <vN>` | View a specific version (default latest). |
| `--versions` | Show the version history instead of the page. |
| `--files` | Show the file list instead of the page. |

```bash
loradex view keenan/flux2-klein-portrait
loradex view keenan/flux2-klein-portrait --versions
loradex view keenan/flux2-klein-portrait --version v2 --files
```

---

### Fetch

#### `loradex pull <owner>/<repo>`

Download model file(s) for use in a pipeline (hash-verified, path-confined).

| Flag | Description |
| --- | --- |
| `-o, --output <dir>` | Output directory (default `.`). |
| `--version <vN>` | Version to pull (default latest). |
| `--include-samples` | Also download sample images. |
| `--force` | Overwrite existing files. |

```bash
loradex pull keenan/flux2-klein-portrait
loradex pull keenan/flux2-klein-portrait -o ./models/ --version v2
loradex pull keenan/flux2-klein-portrait --include-samples --force
```

#### `loradex clone <owner>/<repo>`

Create a full, editable working copy (weights + metadata + history) ready to re-publish.

| Flag | Description |
| --- | --- |
| `-o, --output <dir>` | Output directory (default `./<repo>`). |
| `--version <vN>` | Version to clone (default latest). |
| `--no-samples` | Skip sample images. |
| `--force` | Clone into a non-empty directory. |

```bash
loradex clone keenan/flux2-klein-portrait
loradex clone keenan/flux2-klein-portrait -o ./my-copy --no-samples
```

---

### Projects

#### `loradex init [name]`

Create a project (a managed workspace) via a short wizard. By default the project is created
under `~/.loradex/projects/<name>/` and becomes your active project. Run with no flags for the
interactive wizard, or pass flags to skip it.

| Flag | Description |
| --- | --- |
| `--name <slug>` | Project name/slug (also positional: `loradex init my-lora`). |
| `--description <text>` | Project description. |
| `--base <id>` | Base model for the first model (`flux2-klein`/`flux1`/`sdxl`/`sd15`). |
| `--trigger <word>` | Trigger token for the first model. |
| `--format <fmt>` | Weights format (default `safetensors`). |
| `--license <id>` | License id. |
| `--private` / `--public` | Visibility of the first model (default public). |
| `--from <file>` | Import an existing `.safetensors` as `v1`. |
| `--here` | Scaffold a workspace in the current directory instead of the managed projects folder. |
| `--force` | Overwrite an existing project/workspace. |

```bash
loradex init                                          # interactive wizard
loradex init portraits --base flux2-klein --trigger ohwxman -y
loradex init flux2-klein-portrait --base flux2-klein --from ./model.safetensors
loradex init . --here --base sdxl                     # workspace in the current dir
```

#### `loradex projects`

List managed projects under `~/.loradex/projects` (`*` marks the active one). No flags.

```bash
loradex projects
loradex projects --json
```

#### `loradex use <name>`

Set the active managed project. No flags.

```bash
loradex use portraits
```

#### `loradex status`

Show workspace models, versions, and push state for the active project (or `--path`).

| Flag | Description |
| --- | --- |
| `--path <dir>` | Workspace directory (default: active project / current dir). |

```bash
loradex status
loradex status --path ~/.loradex/projects/portraits
```

---

### Training

#### `loradex build [images]`

Train a LoRA locally by orchestrating ai-toolkit, then collect the output into a versioned,
pushable model folder. Pass a folder of images, or omit it to retrain the project's dataset.

**Interactive wizard.** Run on a terminal, `loradex build [images]` opens a wizard: every
setting (base model, caption model, trigger, steps, rank, alpha, learning rate, optimizer,
precision, resolution, name) is listed and prefilled with sensible defaults. Arrow-key to a
setting and edit it — picking from a list where there's a fixed set of choices, or typing a
validated value (e.g. steps) where there isn't — each with a one-line description and example.
Choose **Start training** and loradex then:

1. **captions** the dataset with the interpreter, showing a progress bar;
2. shows a **caption preview** (filename → caption) to confirm;
3. shows the **output files** that will be written (full paths) to confirm;
4. **trains**, with a step progress bar, `done/total`, and estimated time remaining.

Pass flags (or `-y` / `--json`) to skip the wizard for scripted runs.

**Workflow flags**

| Flag | Description |
| --- | --- |
| `--base <id>` | Base model (e.g. `flux2-klein`). Defaults to the project's base. |
| `--trigger <token>` | Trigger token baked into captions. |
| `--interpreter <id>` | Caption model to auto-caption the dataset (default: `default-interpreter`). |
| `--type <kind>` | `subject` (default), `style`, or `concept`. |
| `--name <slug>` | Repo/output slug (default `<project>-<base>`). |
| `--dataset <dir>` | Use this folder as the dataset (external, not ingested). |
| `--caption <mode>` | `auto`, `keep`, or `none`. |
| `--path <dir>` | Workspace root (default: active project / current dir). |
| `--init` | Auto-init a workspace if one isn't found. |
| `--dry-run` | Print the training plan and exit (no training). |
| `--resume` | Resume the last interrupted run from its checkpoints. |

**Base-model source**

| Flag | Description |
| --- | --- |
| `--checkpoint <path\|id>` | Base model path or HF id — overrides the built-in mapping (use a local model). |
| `--config <file>` | Raw ai-toolkit config file (escape hatch; skips generation). |
| `--trainer <name>` | Training backend (default `ai-toolkit`). |
| `--device <dev>` | `auto` (default), `mps`, `cpu`, or `cuda`. |

**Hyperparameters** (all optional; sensible per-base defaults otherwise)

| Flag | Description |
| --- | --- |
| `--steps <n>` | Training steps (default: auto from image count). |
| `--rank <n>` / `--alpha <n>` | LoRA rank / alpha. |
| `--lr <f>` | Learning rate. |
| `--batch <n>` / `--grad-accum <n>` | Batch size / gradient accumulation. |
| `--optimizer <name>` | `adamw8bit`, `adafactor`, or `prodigy`. |
| `--precision <p>` | `bf16`, `fp16`, or `fp32`. |
| `--resolution <n>` | Training resolution. |
| `--no-bucketing` | Disable aspect-ratio bucketing. |
| `--seed <n>` | Random seed. |
| `--save-every <n>` | Checkpoint/sample interval. |
| `--samples <n>` | Number of validation samples to generate. |
| `--train-text-encoder` | Also train the text encoder. |
| `--profile <name>` | Named profile from your loradex config. |

```bash
loradex build ./photos --base flux2-klein --trigger ohwxman
loradex build ./photos --base flux2-klein --dry-run
loradex build --base sdxl                       # retrain the project dataset on another base
loradex build --base flux2-klein --resume
loradex build ./photos --base flux2-klein --rank 32 --steps 1200 --lr 1e-4
loradex build ./photos --base flux2-klein --checkpoint ~/.loradex/models/flux2-klein
```

---

### Base models

#### `loradex models`

Browse and download base models for training. With no subcommand it opens an interactive
browser (accepts a number, a model id, or a pasted `pull <id>` command). Also reachable as
`loradex --models`.

```bash
loradex models                       # interactive browser
```

##### `loradex models list`

List base models and their download status (`--json` supported). No flags.

##### `loradex models pull <id>...`

Download one or more base models into the loradex models directory. Gated models (e.g.
FLUX.1-dev) prompt for a HuggingFace login first; partial downloads resume; an
already-downloaded model prompts to delete + re-download.

| Flag | Description |
| --- | --- |
| `--force` | Remove any existing copy and re-download from scratch. |

```bash
loradex models pull flux2-klein            # FLUX.2 Klein 4B (open, no login)
loradex models pull sdxl sd15              # several at once
loradex models pull flux2-klein --force    # wipe + re-download
```

##### `loradex models add`

Catalog a custom base model (HuggingFace repo or direct https URL). With no flags it prompts
interactively.

| Flag | Description |
| --- | --- |
| `--id <slug>` | Unique slug / base id (required). |
| `--name <text>` | Display name. |
| `--arch <a>` | `flux2`, `flux1`, `sdxl`, `sd15`, or `other`. |
| `--repo <id>` | HuggingFace repo id (use this OR `--url`). |
| `--url <https>` | Direct https URL to a single file. |
| `--sha256 <hex>` | Integrity digest for URL downloads (recommended). |
| `--format <fmt>` | `diffusers` or `safetensors`. |
| `--license <id>` | License id. |
| `--size-gb <f>` | Approximate size in GB (informational). |
| `--gated` | Mark as requiring HuggingFace auth. |

```bash
loradex models add                                              # interactive
loradex models add --id my-flux --repo org/My-FLUX --arch flux2
loradex models add --id my-lora --url https://host/model.safetensors --sha256 abc…
```

##### `loradex models rm <id>`

Remove a downloaded model (and its custom catalog entry, if any). No flags.

##### `loradex models path <id>`

Print the local path of a downloaded model (for scripting). No flags.

```bash
loradex models path flux2-klein
```

---

### Caption models (interpreters)

Before training, `loradex build` can auto-generate a **detailed caption for every
image** using a vision-language "interpreter" model — so the LoRA learns from
descriptive prompts, not just the trigger word. Captions are written as `<image>.txt`
sidecars (with the trigger prepended). Interpreters are pulled and stored just like
base models, under `~/.loradex/interpreters`.

#### `loradex interpreters`

Browse/download caption models. The first one you pull becomes your **default
captioner**. Built-ins: `qwen3-vl-4b` (default), `qwen3-vl-8b`, `qwen2.5-vl-7b`,
`qwen2.5-vl-3b`. Subcommands mirror `models`: `list`, `pull <id>`, `add`, `rm`, `path`.

```bash
loradex interpreters                              # interactive browser
loradex interpreters pull qwen3-vl-4b             # ~9GB → becomes the default
loradex interpreters add --id my-vlm --repo org/My-VLM
```

Captioning runs in your trainer's Python (torch + transformers); `loradex setup`
already provides it. `loradex build` captions automatically when `--caption auto`
(the default once a captioner exists); pick a model per-run with `--interpreter <id>`.

```bash
loradex build ./photos --base flux2-klein --trigger ohwxman          # auto-captions
loradex build ./photos --interpreter qwen3-vl-8b --caption auto       # a different captioner
loradex build ./photos --caption keep                                 # use existing .txt
loradex build ./photos --caption none                                 # trigger-only
```

### Configuration (`loradex config`)

View the home/paths/defaults and set the global default base model and caption model.

```bash
loradex config                                    # show config + defaults
loradex config set default-base flux2-klein
loradex config set default-interpreter qwen3-vl-4b
```

`build` resolves the base as `--base` > project default > `default-base`; the
captioner as `--interpreter` > `default-interpreter`.

---

### Import (Draw Things)

#### `loradex import [file]`

Import a LoRA you trained in Draw Things (a `.ckpt` SQLite checkpoint) as a publishable
`drawthings`-format model. Base, rank, trigger, and name are auto-detected. A bare filename is
resolved against your Draw Things models folder.

| Flag | Description |
| --- | --- |
| `--list` | List Draw Things LoRAs detected on this machine and exit. |
| `--base <id>` | Base model id (default: auto-detected). |
| `--name <slug>` | Model/repo slug (default: auto-detected). |
| `--trigger <token>` | Trigger token (default: auto-detected). |
| `--rank <n>` | LoRA network rank (default: auto-detected). |
| `--license <id>` | License id. |
| `--private` | Make the model private. |
| `--path <dir>` | Workspace root (scaffolded if missing). |

```bash
loradex import --list
loradex import keenan_v4_1500_lora_f32.ckpt          # bare name → Draw Things folder
loradex import ./my_lora.ckpt --base flux2-klein --trigger ohwxman --name keenan-v4
```

---

### Convert

#### `loradex convert [file]`

Convert a LoRA between formats. With no file, pick one from the active project interactively.
Output is written next to the source under a per-format subfolder
(e.g. `…/versions/vN/mlx/<file>`). Conversions run in a managed Python venv (`~/.loradex/tools`,
auto-installed on first use).

| Flag | Description |
| --- | --- |
| `--to <fmts>` | Target format(s), comma-separated: `mlx`, `diffusers`, `drawthings`, `safetensors`. |
| `--from <fmt>` | Override the detected source format. |
| `--output <dir>` | Output base dir (default: alongside the source). |

**Reliability:** safetensors↔MLX are faithful; diffusers is a best-effort key remap; Draw
Things read/write are experimental (float32 LoRAs only).

```bash
loradex convert                                      # pick a file + targets interactively
loradex convert lora.safetensors --to mlx
loradex convert lora.safetensors --to mlx,diffusers
loradex convert keenan.ckpt --to safetensors         # Draw Things → safetensors
loradex convert lora.safetensors --to mlx --output ./exports
```

---

### Publish

#### `loradex push [models/<base>[@vN]]`

Publish a version folder (weights + README + config.json + samples) as a new immutable remote
version. Identical content no-ops. Targets the active project's latest version by default.

| Flag | Description |
| --- | --- |
| `-m, --message <text>` | Release notes. |
| `--include-samples` | Also upload images under the version's `samples/`. |
| `--create` | Auto-create the repo if missing (default `true`; `--create=false` to require it). |
| `--force` | Allow weights resolving outside your home directory. |
| `--dry-run` | Print the plan without uploading or mutating. |
| `--path <dir>` | Workspace directory (default: active project / current dir). |

```bash
loradex push                                  # single-model project → latest version
loradex push models/flux2-klein               # latest version of that base
loradex push models/sdxl@v2 -m "Better eyes"
loradex push models/flux2-klein --dry-run
```

---

### Setup & maintenance

#### `loradex setup`

Configure loradex: detect/install training backends (ai-toolkit, Draw Things, ComfyUI) and the
HuggingFace CLI, record their locations, and optionally add loradex to your PATH. Safe to
re-run. Interactive checklist by default.

| Flag | Description |
| --- | --- |
| `--install-aitoolkit` | Install ai-toolkit under the loradex home (non-interactive). |
| `--install-hf` | Install the HuggingFace CLI (non-interactive). |
| `--reinstall-aitoolkit` | Remove and reinstall ai-toolkit. |
| `--add-path` | Add loradex to PATH without prompting. |
| `--non-interactive` | No prompts; record detected components. |

```bash
loradex setup                                              # interactive
loradex setup --install-aitoolkit --install-hf --add-path -y
LORADEX_HOME=~/loradex loradex setup                       # custom home
```

#### `loradex uninstall`

Remove loradex and all its files — the home folder(s) (binary, config, credentials, models,
trainers) — and clean the lines loradex added to your shell profile. Confirms first. No flags
(use `-y` to skip the confirmation).

```bash
loradex uninstall
loradex uninstall -y
```

#### `loradex completion <shell>`

Generate a shell completion script for `bash`, `zsh`, `fish`, or `powershell`.

```bash
loradex completion zsh > "${fpath[1]}/_loradex"      # zsh
loradex completion bash | sudo tee /etc/bash_completion.d/loradex
```

#### `loradex version`

Print the CLI version and build info. No flags.

```bash
loradex version
loradex version --json
```

---

## Configuration & environment

Config and credentials live under the home folder (`$LORADEX_HOME`, default `~/.loradex`):

| File | Contents |
| --- | --- |
| `config.yaml` | Non-secret settings: endpoint, profiles, trainers, projects, base-model registry. |
| `credentials.json` | Per-endpoint CLI tokens (0600). |
| `models/` | Downloaded base models. |
| `projects/` | Managed projects. |
| `trainers/` | Installed trainer backends (e.g. ai-toolkit). |
| `tools/` | The conversion venv. |

**Environment variables:** `LORADEX_HOME`, `LORADEX_ENDPOINT`, `LORADEX_WEB`, `LORADEX_TOKEN`,
`LORADEX_PROFILE`, `LORADEX_MODELS_DIR`, `LORADEX_AITOOLKIT_HOME`, `NO_COLOR`.

---

## Exit codes

Stable contract for scripting:

| Code | Meaning |
| --- | --- |
| `0` | OK |
| `1` | Generic / unexpected error |
| `2` | Bad flags/args (usage) |
| `3` | Unauthorized (401) |
| `4` | Forbidden (403) |
| `5` | Not found (404) |
| `6` | Conflict (409) |
| `7` | Local validation error |
| `8` | Content-hash mismatch |
| `9` | Quota exceeded (413) |
| `10` | Network/transport error |

---

## Security model

- **Endpoint-keyed credentials** — a token is only ever sent to the endpoint it was issued for.
- **HTTPS-only** — `--insecure` permits `http://` only for `127.0.0.1` loopback (login), and
  never disables TLS verification.
- **Hash-verified transfers** — downloads are checked against their declared SHA-256; uploads
  send file bytes directly to/from presigned URLs, never through the API.
- **Path confinement** — extracted/written paths are validated to stay within their destination;
  symlinks are refused.
- **No secret leakage** — loradex credentials are stripped from every subprocess environment
  (trainers, downloaders, converters); verbose logging redacts secrets.

---

## Development

```bash
make build      # build ./loradex
make test       # go test ./... -race -cover
make lint       # go vet + gofmt (+ golangci-lint if present)
make cross      # cross-compile dist/ for all platforms
```

## License

See [LICENSE](LICENSE).
