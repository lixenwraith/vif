"""Merge a training round: average the models of runs that all started from one
--init into the next round's start and a weight file (README, Training in parallel):

    python3 average.py --pt round1.pt --out round1.bin shard0.pt shard1.pt shard2.pt
"""
from __future__ import annotations

import argparse

import torch

import ripmodel


def shape(ck: dict) -> dict:
    return {**{f"cfg.{k}": v for k, v in ck["cfg"].items()}, **{k: tuple(t.shape) for k, t in ck["model"].items()}}


def mean(states: list, cut: int) -> dict:
    """each weight's mean over the states, its cut highest and lowest values dropped first"""
    out = {}
    for k in states[0]:
        w = torch.stack([s[k] for s in states])
        if cut:
            w = w.sort(0).values[cut:len(states) - cut]
        out[k] = w.mean(0)
    return out


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("files", nargs="+", help="the round's checkpoints, each train.py's --checkpoint")
    ap.add_argument("--pt", required=True, help="the mean as a .pt, the next round's --init")
    ap.add_argument("--out", required=True, help="the mean as the weight file lixenwraith/transformer loads, as train.py's --out")
    ap.add_argument("--trim", type=float, default=0.0, help="share of the files whose highest and lowest values of each weight are dropped before the mean, under a half")
    args = ap.parse_args()
    if not 0 <= args.trim < 0.5:
        ap.error("--trim is a share under a half")

    cks = [torch.load(f, map_location="cpu") for f in args.files]
    want = None
    for f, ck in zip(args.files, cks):
        if "cfg" not in ck:
            raise SystemExit(f"{f} records no model shape: not a checkpoint of this train.py")
        got = shape(ck)
        want = want or got
        diff = sorted(k for k in want.keys() | got.keys() if want.get(k) != got.get(k))
        if diff:
            raise SystemExit(f"{f}: {diff[0]} is {got.get(diff[0])}, {args.files[0]}'s {want.get(diff[0])}; a round's files share one shape")

    cut = int(args.trim * len(cks))
    model = ripmodel.RipModel(ripmodel.Config(**cks[0]["cfg"]))
    model.load_state_dict(mean([ck["model"] for ck in cks], cut))
    torch.save({"model": model.state_dict(), "cfg": cks[0]["cfg"]}, args.pt)
    model.eval()
    ripmodel.export(model, args.out)
    print(f"averaged {len(cks)} files, {cut} dropped at each end of each weight; wrote {args.pt} and {args.out}")


if __name__ == "__main__":
    main()
