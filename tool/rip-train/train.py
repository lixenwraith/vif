"""Train the rip's front end on a corpus soundlab exported (`corpus <dir>`), and
export the weights pkg/transformer loads:

    python3 train.py --corpus corpus/ --out ripnet.bin --steps 2000

Each loop's log-mel frames (loop_NNN.mel) are the input; its events
(loop_NNN.json) become per-frame targets: an onset within a frame of any event,
each drum's hits, the pitches sounding, whether any sounds, the wave and voice
genes of the highest note sounding, and each pitch within a frame of its strikes. Training crops random windows of frames, so
memory is bounded by --window, not by the loops' length.
"""
from __future__ import annotations

import argparse
import json
import math
import os
import random
import struct
import time
from dataclasses import asdict

import numpy as np
import torch
import torch.nn.functional as F

import ripnet


class MelFile:
    """A loop's log-mel frames, read from its file as sliced: the frames of 40000
    loops are 8 GB, which the page cache holds rather than the process."""

    def __init__(self, path: str):
        self.path = path
        with open(path, "rb") as f:
            self.frames, self.bands = struct.unpack("<II", f.read(8))

    def __len__(self) -> int:
        return self.frames

    def __getitem__(self, sl: slice) -> np.ndarray:
        start, stop, _ = sl.indices(self.frames)
        n = max(stop - start, 0)
        return np.fromfile(self.path, dtype="<f4", count=n * self.bands, offset=8 + 4 * start * self.bands).reshape(n, self.bands)


def targets(label: dict, frames: int, manifest: dict) -> dict:
    """Per-frame targets from a loop's events; masks say where a target counts.
    The yes-or-no ones are bytes, a quarter of a float's memory, until batch;
    the strikes stay a list of (frame, pitch), as sparse as they are."""
    pitches, min_midi = manifest["pitches"], manifest["min_midi"]
    genes, waves = manifest["voice_genes"], len(manifest["waves"])
    onset = np.zeros(frames, np.uint8)
    drums = np.zeros((frames, 3), np.uint8)
    pitch = np.zeros((frames, pitches), np.uint8)
    wave = np.full(frames, -1, np.int64)
    voice = np.zeros((frames, genes), np.float32)
    top = np.full(frames, -1, np.int64)  # the highest midi sounding, whose voice the frame reports
    strikes = []
    for e in label["events"]:
        t = e["frame"]
        if t < 0 or t >= frames:
            continue
        onset[max(t - 1, 0):min(t + 2, frames)] = 1
        if e["drum"] >= 0:
            drums[max(t - 1, 0):min(t + 2, frames), e["drum"]] = 1
            continue
        p = e["midi"] - min_midi
        end = min(max(e["end"], t + 1), frames)
        if 0 <= p < pitches:
            pitch[t:end, p] = 1
            strikes.append((t, p))
        if e.get("wave", -1) >= 0 and e.get("voice"):
            for u in range(t, end):
                if e["midi"] > top[u]:
                    top[u], wave[u] = e["midi"], e["wave"]
                    voice[u] = e["voice"]
    voicing = (pitch.sum(1) > 0).astype(np.uint8)
    return {"onset": onset, "drums": drums, "pitch": pitch, "voicing": voicing,
            "wave": wave, "voice": voice, "voiced": (top >= 0).astype(np.uint8),
            "strikes": np.array(strikes, np.int32).reshape(-1, 2)}


def load_corpus(d: str):
    with open(os.path.join(d, "corpus.json")) as f:
        manifest = json.load(f)
    loops = []
    for i in range(manifest["loops"]):
        base = os.path.join(d, f"loop_{i:03d}")
        mel = MelFile(base + ".mel")
        with open(base + ".json") as f:
            label = json.load(f)
        loops.append((mel, targets(label, len(mel), manifest)))
    return manifest, loops


def batch(loops, window: int, size: int, rng: random.Random):
    """size random windows of `window` frames, shorter loops padded with zeros and masked out"""
    mels, tg = [], {k: [] for k in ("onset", "drums", "pitch", "voicing", "wave", "voice", "voiced", "strike", "valid")}
    for _ in range(size):
        mel, t = rng.choice(loops)
        n = len(mel)
        start = rng.randrange(max(n - window, 0) + 1)
        sl = slice(start, start + window)
        pad = max(window - (n - start), 0)
        def take(a, fill=0):
            a = a[sl]
            if pad:
                shape = (pad,) + a.shape[1:]
                a = np.concatenate([a, np.full(shape, fill, a.dtype)])
            return a
        mels.append(take(mel))
        for k in ("onset", "drums", "pitch", "voicing", "voice", "voiced"):
            tg[k].append(take(t[k]))
        tg["wave"].append(take(t["wave"], -1))
        strike = np.zeros((window, t["pitch"].shape[1]), np.uint8)
        for f, p in t["strikes"]:
            strike[max(f - 1 - start, 0):max(min(f + 2 - start, window), 0), p] = 1
        tg["strike"].append(strike)
        tg["valid"].append(np.concatenate([np.ones(window - pad, np.float32), np.zeros(pad, np.float32)]))
    out = {k: torch.from_numpy(np.stack(v)) for k, v in tg.items()}
    return torch.from_numpy(np.stack(mels)), {k: v if k == "wave" else v.float() for k, v in out.items()}


def validation(loops, window: int):
    """every held-out loop cut into windows of `window` frames, fixed, so the held-out loss is comparable step to step"""
    rng = random.Random(0)
    mels, tgs = [], []
    for mel, t in loops:
        for start in range(0, max(len(mel) - 1, 1), window):
            cut = {k: v[start:start + window] for k, v in t.items() if k != "strikes"}
            cut["strikes"] = t["strikes"] - np.array([start, 0], np.int32)
            sub = [(mel[start:start + window], cut)]
            m, tg = batch(sub, window, 1, rng)
            mels.append(m)
            tgs.append(tg)
    return mels, tgs


def evaluate(model: ripnet.RipNet, mels, tgs, device: str) -> dict:
    """the mean of the losses over the held-out windows, the model's weights untouched"""
    model.eval()
    sums: dict = {}
    with torch.no_grad():
        for mel, t in zip(mels, tgs):
            l = losses(model, mel.to(device), {k: v.to(device) for k, v in t.items()})
            for k, v in l.items():
                sums[k] = sums.get(k, 0.0) + v.item()
    model.train()
    return {k: v / max(len(mels), 1) for k, v in sums.items()}


def losses(model: ripnet.RipNet, mel: torch.Tensor, t: dict) -> dict:
    y = model(mel)
    cfg = model.cfg
    valid = t["valid"]
    def bce(name, target, pos_weight):
        logits = y[..., cfg.slice(name)]
        target = target if target.dim() == 3 else target[..., None]
        l = F.binary_cross_entropy_with_logits(logits, target, pos_weight=torch.tensor(pos_weight), reduction="none")
        return (l.mean(-1) * valid).sum() / valid.sum().clamp(min=1)
    out = {
        "onset": bce("onset", t["onset"], 8.0),
        "drums": bce("drums", t["drums"], 8.0),
        "pitch": bce("pitch", t["pitch"], 12.0),
        "voicing": bce("voicing", t["voicing"], 2.0),
        "strike": bce("strike", t["strike"], 20.0),
    }
    voiced = t["voiced"] * valid
    n = voiced.sum().clamp(min=1)
    wave_logits = y[..., cfg.slice("wave")].reshape(-1, ripnet.WAVES)
    wave_target = t["wave"].reshape(-1).clamp(min=0)
    ce = F.cross_entropy(wave_logits, wave_target, reduction="none").reshape(voiced.shape)
    out["wave"] = (ce * voiced).sum() / n
    voice = torch.sigmoid(y[..., cfg.slice("voice")])
    out["voice"] = ((voice - t["voice"]) ** 2).mean(-1).mul(voiced).sum() / n
    out["total"] = out["onset"] + out["drums"] + out["pitch"] + 0.5 * out["voicing"] + out["strike"] + 0.5 * out["wave"] + 2.0 * out["voice"]
    return out


def shape(cfg) -> str:
    """a model's sizes for a message; the output heads are ripnet's, never a flag's"""
    return ", ".join(f"{k} {v}" for k, v in cfg.items() if k != "outputs") if cfg else "an unrecorded shape"


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--corpus", required=True, help="directory soundlab's corpus verb wrote")
    ap.add_argument("--out", required=True, help="weight file to write, the root's audio/ripnet.bin for soundlab to hear with")
    ap.add_argument("--steps", type=int, default=2000)
    ap.add_argument("--batch", type=int, default=8)
    ap.add_argument("--window", type=int, default=512, help="frames per training window (11.6 ms each)")
    ap.add_argument("--lr", type=float, default=3e-4)
    ap.add_argument("--dim", type=int, default=256)
    ap.add_argument("--layers", type=int, default=4)
    ap.add_argument("--heads", type=int, default=8)
    ap.add_argument("--ffn", type=int, default=1024)
    ap.add_argument("--towers", default="", help="stacks of their own after the shared input, layers:heads each in the heads' order, comma-separated: 2:2,4:5 gives onset and drums two layers and the rest four")
    ap.add_argument("--convs", default="", help="convolutions before the input layer, channels x frames x features / pool each, comma-separated: 16x3x3/1,16x3x3/2,32x3x3/2")
    ap.add_argument("--attention-window", type=int, default=64, help="frames attended either side")
    ap.add_argument("--seed", type=int, default=1)
    ap.add_argument("--shard-seed", type=int, default=0, help="offsets the stream the windows are drawn from, so a round's workers see different loops; the starting weights stay --seed's")
    ap.add_argument("--checkpoint", default="", help="write a .pt every --every steps, and resume from it when present")
    ap.add_argument("--init", default="", help="start from this .pt's model weights, without its optimizer state or step: a training round's common start; a --checkpoint present wins")
    ap.add_argument("--every", type=int, default=200, help="steps between checkpoints and held-out scores")
    ap.add_argument("--holdout", type=int, default=-1, help="loops held out of training and scored every --every steps; the step that scores best is what --out gets (default a tenth, none under five loops)")
    ap.add_argument("--device", default="cuda" if torch.cuda.is_available() else "cpu")
    args = ap.parse_args()

    torch.manual_seed(args.seed)
    rng = random.Random(args.seed + (args.shard_seed << 32))  # a stream per seed and shard; shard 0 is the seed's own
    convs = [tuple(int(v) for v in c.replace("/", "x").split("x")) for c in args.convs.split(",") if c]
    if any(len(c) != 4 for c in convs):
        raise SystemExit(f"--convs {args.convs!r}: want channels x frames x features / pool, comma-separated")
    towers = [tuple(int(v) for v in t.split(":")) for t in args.towers.split(",") if t]
    if any(len(t) != 2 for t in towers) or towers and sum(h for _, h in towers) != len(ripnet.HEADS):
        raise SystemExit(f"--towers {args.towers!r}: want layers:heads each, the heads adding up to {len(ripnet.HEADS)}")
    cfg = ripnet.Config(convs=convs, dim=args.dim, layers=args.layers, heads=args.heads, ffn=args.ffn, window=args.attention_window, towers=towers)
    resume = bool(args.checkpoint) and os.path.exists(args.checkpoint)
    start = args.checkpoint if resume else args.init
    ck = torch.load(start, map_location=args.device) if start else None
    # refused before the corpus loads: load_state_dict names only a tensor's size,
    # and takes another head count or window without a word
    if ck is not None and ck.get("cfg") != asdict(cfg):
        raise SystemExit(f"{start} holds a model of {shape(ck.get('cfg'))}, and this run builds {shape(asdict(cfg))}: pass its shape, or another file")
    manifest, loops = load_corpus(args.corpus)
    holdout = args.holdout if args.holdout >= 0 else (len(loops) // 10 if len(loops) >= 5 else 0)
    held = loops[len(loops) - holdout:] if holdout else []
    loops = loops[:len(loops) - holdout] if holdout else loops
    val_mels, val_tgs = validation(held, args.window) if held else ([], [])
    assert manifest["mel_bands"] == ripnet.MEL_BANDS and manifest["pitches"] == ripnet.PITCHES
    assert manifest["voice_genes"] == ripnet.VOICE_GENES and len(manifest["waves"]) == ripnet.WAVES
    model = ripnet.RipNet(cfg).to(args.device)
    opt = torch.optim.AdamW(model.parameters(), lr=args.lr, weight_decay=0.01)
    step, best_loss, best_step, best_state = 0, float("inf"), 0, None
    if resume:
        model.load_state_dict(ck["model"])
        opt.load_state_dict(ck["opt"])
        step = ck["step"]
        best_loss, best_step, best_state = ck.get("best", (best_loss, best_step, best_state))
        print(f"resumed at step {step}")
    elif ck is not None:
        model.load_state_dict(ck["model"])
        print(f"started from {args.init}")
    print(f"{sum(p.numel() for p in model.parameters())} weights, {len(loops)} loops, {sum(len(m) for m, _ in loops)} frames, {len(held)} held out in {len(val_mels)} windows")
    t0 = time.time()

    def score() -> None:
        nonlocal best_loss, best_step, best_state
        if not val_mels:
            return
        v = evaluate(model, val_mels, val_tgs, args.device)
        # the decoders read the onset, drum, pitch and voicing heads; the wave and
        # voice heads name a voice and memorize the corpus's, so they do not pick
        # the step exported
        v["decoders"] = v["onset"] + v["drums"] + v["pitch"] + 0.5 * v["voicing"] + v["strike"]
        parts = " ".join(f"{k} {x:.3f}" for k, x in v.items() if k not in ("total", "decoders"))
        mark = ""
        if v["decoders"] < best_loss:
            best_loss, best_step = v["decoders"], step
            best_state = {k: t.detach().cpu().clone() for k, t in model.state_dict().items()}
            mark = " (best)"
        print(f"step {step} held-out {v['total']:.3f}, decoders {v['decoders']:.3f} ({parts}){mark}", flush=True)

    while step < args.steps:
        mel, t = batch(loops, args.window, args.batch, rng)
        mel, t = mel.to(args.device), {k: v.to(args.device) for k, v in t.items()}
        lr = args.lr * 0.5 * (1 + math.cos(math.pi * step / args.steps))  # cosine to zero
        for g in opt.param_groups:
            g["lr"] = lr
        l = losses(model, mel, t)
        opt.zero_grad()
        l["total"].backward()
        torch.nn.utils.clip_grad_norm_(model.parameters(), 1.0)
        opt.step()
        step += 1
        if step % 20 == 0 or step == args.steps:
            parts = " ".join(f"{k} {v.item():.3f}" for k, v in l.items() if k != "total")
            print(f"step {step} loss {l['total'].item():.3f} ({parts}) {time.time() - t0:.0f}s", flush=True)
        if step % args.every == 0 or step == args.steps:
            score()
            if args.checkpoint:
                # the shape rides along for average.py and the check above, the heads and
                # the window showing in no tensor; the best step, so a resumed run, or one
                # run again once done, still exports it
                torch.save({"model": model.state_dict(), "opt": opt.state_dict(), "step": step, "cfg": asdict(cfg),
                            "best": (best_loss, best_step, best_state)}, args.checkpoint)
    if best_state is not None:
        model.load_state_dict(best_state)
        print(f"best held-out decoders' loss {best_loss:.3f} at step {best_step}; exporting that")
    model.eval()
    ripnet.export(model.cpu(), args.out)
    print(f"wrote {args.out}")


if __name__ == "__main__":
    main()
