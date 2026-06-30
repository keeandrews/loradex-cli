#!/usr/bin/env python3
"""loradex format converter.

Reads a LoRA from one format into a common dict[str, np.ndarray] (+ metadata),
then writes it to a target format. Invoked by the loradex CLI as:

    python convert.py <src_path> <src_fmt> <dst_fmt> <out_path>

Formats: safetensors | mlx | diffusers | drawthings
Emits a single JSON line on stdout: {"ok", "tensors", "warnings", "quality"}.

Reliability: safetensors/mlx are faithful (same tensors, MLX-validated container).
diffusers is a best-effort key remap. drawthings read/write are EXPERIMENTAL
(Draw Things uses a proprietary SQLite/NNC layout; only float32 LoRAs are handled).
"""
import json
import sqlite3
import struct
import sys

import numpy as np

WARNINGS = []


def warn(msg):
    WARNINGS.append(msg)


# ---------- readers ----------

def read_safetensors(path):
    from safetensors import safe_open
    tensors, meta = {}, {}
    with safe_open(path, framework="numpy") as f:
        meta = f.metadata() or {}
        for k in f.keys():
            tensors[k] = f.get_tensor(k)
    return tensors, meta


def _parse_dim(blob):
    if not blob or len(blob) < 8:
        return []
    n = len(blob) // 4
    vals = struct.unpack("<%di" % n, blob[: n * 4])
    dims = []
    for v in vals[1:]:  # vals[0] is a format/type header
        if v <= 0:
            break
        dims.append(v)
    return dims


def read_drawthings(path):
    """EXPERIMENTAL: extract float32 tensors from a Draw Things .ckpt (SQLite)."""
    warn("Draw Things read is experimental — only float32 LoRAs are supported")
    con = sqlite3.connect("file:%s?mode=ro" % path, uri=True)
    tensors = {}
    skipped = 0
    for name, dim, data in con.execute("SELECT name, dim, data FROM tensors"):
        shape = _parse_dim(dim)
        count = int(np.prod(shape)) if shape else 0
        if count <= 0 or not data or len(data) < count * 4:
            skipped += 1
            continue
        raw = bytes(data)[len(data) - count * 4:]  # tail bytes = raw f32 payload
        try:
            arr = np.frombuffer(raw, dtype="<f4").reshape(shape).astype(np.float32)
        except Exception:
            skipped += 1
            continue
        tensors[name] = arr
    con.close()
    if skipped:
        warn("skipped %d tensor(s) that weren't decodable as float32" % skipped)
    if not tensors:
        raise RuntimeError("no float32 tensors could be decoded from the Draw Things file")
    return tensors, {"source": "drawthings"}


# ---------- writers ----------

def write_safetensors(tensors, meta, path):
    from safetensors.numpy import save_file
    md = {k: str(v) for k, v in (meta or {}).items()}
    save_file({k: np.ascontiguousarray(v) for k, v in tensors.items()}, path, metadata=md or None)


def write_mlx(tensors, meta, path):
    """Re-container as an MLX-validated safetensors (every tensor round-tripped
    through mx.array, so MLX is guaranteed to load it)."""
    try:
        import mlx.core as mx
    except Exception as e:
        raise RuntimeError("MLX is not available on this machine (%s)" % e)
    arrs = {k: mx.array(np.ascontiguousarray(v)) for k, v in tensors.items()}
    md = {k: str(v) for k, v in (meta or {}).items()}
    mx.save_safetensors(path, arrs, metadata=md or {})


def _to_diffusers_key(k):
    # Best-effort kohya -> diffusers/PEFT renaming.
    k = k.replace("lora_unet_", "").replace("lora_te_", "te_")
    k = k.replace(".lora_down.weight", ".lora_A.weight")
    k = k.replace(".lora_up.weight", ".lora_B.weight")
    k = k.replace("_lora_down", ".lora_A").replace("_lora_up", ".lora_B")
    return k


def write_diffusers(tensors, meta, path):
    warn("diffusers conversion remaps keys best-effort; verify against your loader")
    remapped, alpha = {}, 0
    for k, v in tensors.items():
        if k.endswith(".alpha"):
            alpha += 1
            continue  # diffusers/PEFT derives scaling from rank, not an alpha tensor
        remapped[_to_diffusers_key(k)] = v
    if alpha:
        warn("dropped %d .alpha tensor(s) (not used by diffusers/PEFT)" % alpha)
    write_safetensors(remapped, {"format": "diffusers"}, path)


def write_drawthings(tensors, meta, path):
    """EXPERIMENTAL: write float32 tensors into a Draw Things-style SQLite. The
    resulting file may not load in Draw Things (its loader expects a specific NNC
    layout + a loratrainingconfiguration record). Provided for round-tripping."""
    warn("Draw Things write is EXPERIMENTAL and may not load in Draw Things itself")
    con = sqlite3.connect(path)
    con.execute(
        "CREATE TABLE tensors (name TEXT, type INTEGER, format INTEGER, "
        "datatype INTEGER, dim BLOB, data BLOB, PRIMARY KEY (name))"
    )
    for name, v in tensors.items():
        v = np.ascontiguousarray(v.astype(np.float32))
        dims = list(v.shape)
        dim_blob = struct.pack("<i", 0x00000C00) + b"".join(struct.pack("<i", d) for d in dims)
        dim_blob += struct.pack("<i", 0) * (8 - len(dims))
        con.execute(
            "INSERT INTO tensors (name, type, format, datatype, dim, data) VALUES (?,?,?,?,?,?)",
            (name, 0, 0, 1, dim_blob, v.tobytes()),
        )
    con.commit()
    con.close()


READERS = {"safetensors": read_safetensors, "drawthings": read_drawthings}
WRITERS = {
    "safetensors": write_safetensors,
    "mlx": write_mlx,
    "diffusers": write_diffusers,
    "drawthings": write_drawthings,
}


def main():
    if len(sys.argv) != 5:
        print(json.dumps({"ok": False, "error": "usage: convert.py <src> <src_fmt> <dst_fmt> <out>"}))
        return 2
    src, src_fmt, dst_fmt, out = sys.argv[1:5]
    if src_fmt not in READERS:
        print(json.dumps({"ok": False, "error": "unsupported source format %r" % src_fmt}))
        return 2
    if dst_fmt not in WRITERS:
        print(json.dumps({"ok": False, "error": "unsupported target format %r" % dst_fmt}))
        return 2
    try:
        tensors, meta = READERS[src_fmt](src)
        WRITERS[dst_fmt](tensors, meta, out)
    except Exception as e:  # noqa: BLE001
        print(json.dumps({"ok": False, "error": str(e), "warnings": WARNINGS}))
        return 1
    if src_fmt == "drawthings" or dst_fmt == "drawthings":
        quality = "experimental"
    elif dst_fmt == "diffusers":
        quality = "best-effort"
    else:
        quality = "faithful"
    print(json.dumps({
        "ok": True,
        "tensors": len(tensors),
        "warnings": WARNINGS,
        "quality": quality,
    }))
    return 0


if __name__ == "__main__":
    sys.exit(main())
