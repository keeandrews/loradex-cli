#!/usr/bin/env python3
"""loradex dataset captioner.

Runs a vision-language model (interpreter) over a folder of images and writes a
`<stem>.txt` caption next to each one, for LoRA training. Invoked by the CLI as:

    python caption.py <model_path> <image_dir> <prompt> [trigger]

When a trigger is given it is prepended to each caption ("<trigger> <desc>") so
the LoRA binds the concept to the trigger token. Emits one JSON line per image
plus a final summary on stdout; progress/errors go to stderr.
"""
import json
import os
import sys

import torch
from PIL import Image

IMAGE_EXTS = {".jpg", ".jpeg", ".png", ".webp", ".bmp"}


def log(msg):
    print(msg, file=sys.stderr, flush=True)


def pick_device():
    if torch.backends.mps.is_available():
        return "mps", torch.bfloat16
    if torch.cuda.is_available():
        return "cuda", torch.bfloat16
    return "cpu", torch.float32


def load_model(model_path, dtype):
    # Qwen2.5-VL / Qwen3-VL register as image-text-to-text; fall back to vision2seq.
    last = None
    for cls_name in ("AutoModelForImageTextToText", "AutoModelForVision2Seq"):
        try:
            import transformers

            cls = getattr(transformers, cls_name)
            return cls.from_pretrained(model_path, torch_dtype=dtype)
        except Exception as e:  # noqa: BLE001
            last = e
    raise RuntimeError(f"could not load interpreter model: {last}")


def main():
    if len(sys.argv) < 4:
        print(json.dumps({"ok": False, "error": "usage: caption.py <model> <dir> <prompt> [trigger]"}))
        return 2
    model_path, image_dir, prompt = sys.argv[1], sys.argv[2], sys.argv[3]
    trigger = sys.argv[4].strip() if len(sys.argv) > 4 else ""

    images = sorted(f for f in os.listdir(image_dir) if os.path.splitext(f)[1].lower() in IMAGE_EXTS)
    if not images:
        print(json.dumps({"ok": True, "captioned": 0, "note": "no images found"}))
        return 0

    device, dtype = pick_device()
    log(f"loading interpreter on {device}…")
    try:
        from transformers import AutoProcessor

        model = load_model(model_path, dtype).to(device).eval()
        # Bound memory on large photos; harmless if the processor ignores it.
        try:
            processor = AutoProcessor.from_pretrained(model_path, max_pixels=1024 * 1024)
        except Exception:  # noqa: BLE001
            processor = AutoProcessor.from_pretrained(model_path)
    except Exception as e:  # noqa: BLE001
        print(json.dumps({"ok": False, "error": f"model load failed: {e}"}))
        return 1

    captioned = 0
    for fn in images:
        path = os.path.join(image_dir, fn)
        txt = os.path.join(image_dir, os.path.splitext(fn)[0] + ".txt")
        try:
            img = Image.open(path).convert("RGB")
            messages = [{"role": "user", "content": [{"type": "image"}, {"type": "text", "text": prompt}]}]
            text = processor.apply_chat_template(messages, tokenize=False, add_generation_prompt=True)
            inputs = processor(text=[text], images=[img], return_tensors="pt").to(device)
            with torch.no_grad():
                out = model.generate(**inputs, max_new_tokens=220, do_sample=False)
            gen = out[0][inputs["input_ids"].shape[1]:]
            caption = processor.decode(gen, skip_special_tokens=True).strip()
        except Exception as e:  # noqa: BLE001
            log(f"  ! {fn}: {e}")
            print(json.dumps({"image": fn, "error": str(e)[:120]}))
            continue
        if trigger and not caption.lower().startswith(trigger.lower()):
            caption = f"{trigger} {caption}"
        with open(txt, "w") as fh:
            fh.write(caption)
        captioned += 1
        print(json.dumps({"image": fn, "caption": caption[:90]}), flush=True)

    print(json.dumps({"ok": True, "captioned": captioned, "total": len(images)}))
    return 0


if __name__ == "__main__":
    sys.exit(main())
