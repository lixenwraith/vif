"""The rip's front-end model as PyTorch builds it, the twin of pkg/audio/ripnet
and pkg/transformer: a linear input, pre-norm attention blocks with ALiBi biases
over a window of frames, exact GELU feed-forwards, a final norm and one output
layer whose slices are the heads. export() writes the weight file pkg/transformer
loads; the tensor names and order are its.
"""
from __future__ import annotations

import math
import struct
from dataclasses import dataclass, field

import torch
import torch.nn as nn
import torch.nn.functional as F

# pkg/audio/ripnet.Spec, which a corpus manifest restates
MEL_BANDS = 80
PITCHES = 69
DRUMS = 3
WAVES = 5
VOICE_GENES = 32

HEADS = [("onset", 1), ("drums", DRUMS), ("pitch", PITCHES), ("voicing", 1), ("wave", WAVES), ("voice", VOICE_GENES)]


@dataclass
class Config:
    inputs: int = MEL_BANDS
    dim: int = 128
    heads: int = 4
    layers: int = 4
    ffn: int = 512
    window: int = 64
    outputs: list = field(default_factory=lambda: list(HEADS))

    @property
    def out_size(self) -> int:
        return sum(n for _, n in self.outputs)

    def slice(self, name: str) -> slice:
        at = 0
        for h, n in self.outputs:
            if h == name:
                return slice(at, at + n)
            at += n
        raise KeyError(name)


class Block(nn.Module):
    def __init__(self, cfg: Config):
        super().__init__()
        self.cfg = cfg
        self.ln1 = nn.LayerNorm(cfg.dim)
        self.q = nn.Linear(cfg.dim, cfg.dim)
        self.k = nn.Linear(cfg.dim, cfg.dim)
        self.v = nn.Linear(cfg.dim, cfg.dim)
        self.o = nn.Linear(cfg.dim, cfg.dim)
        self.ln2 = nn.LayerNorm(cfg.dim)
        self.ff1 = nn.Linear(cfg.dim, cfg.ffn)
        self.ff2 = nn.Linear(cfg.ffn, cfg.dim)
        # head h leans on nearer frames by 2^(-8h/H) per frame of distance
        slopes = torch.tensor([2.0 ** (-8.0 * (h + 1) / cfg.heads) for h in range(cfg.heads)])
        self.register_buffer("slopes", slopes, persistent=False)

    def attend(self, x: torch.Tensor) -> torch.Tensor:
        B, T, D = x.shape
        H, dh = self.cfg.heads, D // self.cfg.heads
        q = self.q(x).view(B, T, H, dh).transpose(1, 2)  # B H T dh
        k = self.k(x).view(B, T, H, dh).transpose(1, 2)
        v = self.v(x).view(B, T, H, dh).transpose(1, 2)
        scores = q @ k.transpose(-1, -2) / math.sqrt(dh)  # B H T T
        pos = torch.arange(T, device=x.device)
        dist = (pos[:, None] - pos[None, :]).abs()  # T T
        scores = scores - self.slopes[None, :, None, None] * dist[None, None].to(scores.dtype)
        scores = scores.masked_fill((dist > self.cfg.window)[None, None], float("-inf"))
        w = scores.softmax(-1)
        out = (w @ v).transpose(1, 2).reshape(B, T, D)
        return self.o(out)

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        x = x + self.attend(self.ln1(x))
        return x + self.ff2(F.gelu(self.ff1(self.ln2(x))))


class RipNet(nn.Module):
    def __init__(self, cfg: Config):
        super().__init__()
        self.cfg = cfg
        self.input = nn.Linear(cfg.inputs, cfg.dim)
        self.layers = nn.ModuleList(Block(cfg) for _ in range(cfg.layers))
        self.ln = nn.LayerNorm(cfg.dim)
        self.out = nn.Linear(cfg.dim, cfg.out_size)

    def forward(self, mel: torch.Tensor) -> torch.Tensor:
        """mel: B T inputs → B T out_size, raw scores; the heads are slices."""
        x = self.input(mel)
        for layer in self.layers:
            x = layer(x)
        return self.out(self.ln(x))


# --- the weight file pkg/transformer loads ---

MAGIC = b"VIFT"
VERSION = 1


def _fnv64a(data: bytes) -> int:
    h = 0xCBF29CE484222325
    for b in data:
        h = ((h ^ b) * 0x100000001B3) & 0xFFFFFFFFFFFFFFFF
    return h


def tensors(model: RipNet):
    """Every weight by the name pkg/transformer gives it, in its order."""
    sd = model.state_dict()
    out = [("in.weight", sd["input.weight"]), ("in.bias", sd["input.bias"])]
    for i in range(model.cfg.layers):
        p = f"layers.{i}."
        for name in ("ln1", "q", "k", "v", "o", "ln2", "ff1", "ff2"):
            out.append((p + name + ".weight", sd[p + name + ".weight"]))
            out.append((p + name + ".bias", sd[p + name + ".bias"]))
    out += [("ln.weight", sd["ln.weight"]), ("ln.bias", sd["ln.bias"]), ("out.weight", sd["out.weight"]), ("out.bias", sd["out.bias"])]
    return out


def export(model: RipNet, path: str) -> None:
    cfg = model.cfg
    body = bytearray(MAGIC)
    body += struct.pack("<I", VERSION)
    body += struct.pack("<6I", cfg.inputs, cfg.dim, cfg.heads, cfg.layers, cfg.ffn, cfg.window)
    body += struct.pack("<I", len(cfg.outputs))
    for name, size in cfg.outputs:
        b = name.encode()
        body += struct.pack("<H", len(b)) + b + struct.pack("<I", size)
    ts = tensors(model)
    body += struct.pack("<I", len(ts))
    for name, t in ts:
        b = name.encode()
        body += struct.pack("<H", len(b)) + b
        dims = list(t.shape)
        body += struct.pack("<B", len(dims)) + struct.pack(f"<{len(dims)}I", *dims)
        body += t.detach().cpu().contiguous().to(torch.float32).numpy().tobytes()
    body += struct.pack("<Q", _fnv64a(bytes(body)))
    with open(path, "wb") as f:
        f.write(body)
