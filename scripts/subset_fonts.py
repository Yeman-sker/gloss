#!/usr/bin/env python3
"""Rebuilds app/fonts from the upstream fonts. See docs/decisions/0006-bundled-fonts.md.

Needs fonttools. Usage: subset_fonts.py <dir with the downloaded fonts> <app/fonts>
The downloaded files are NotoSansSC.ttf, Geist.ttf, GeistMono.ttf and Inter.ttf,
the variable fonts from github.com/google/fonts.
"""
import sys

from fontTools import subset
from fontTools.ttLib import TTFont
from fontTools.varLib import instancer

IPA = "ɡɒæəˈˌɑːʃðɪŋʊʌɔθʒɛɜ/"
QUOTES = "“”‘’"


def gb2312():
    points = set(range(0x20, 0x7F)) | set(range(0xA0, 0x100))
    points |= set(range(0x2000, 0x2070)) | set(range(0x3000, 0x3040)) | set(range(0xFF00, 0xFF70))
    for a in range(0xA1, 0xF8):
        for b in range(0xA1, 0xFF):
            try:
                points.add(ord(bytes([a, b]).decode("gb2312")))
            except UnicodeDecodeError:
                pass
    return points


def gb2312_level1():
    """The 3755 most used characters, GB 2312's first level, and the rest
    of what a strong span holds: ASCII and the CJK punctuation."""
    points = set(range(0x20, 0x7F)) | set(range(0x3000, 0x3040)) | set(range(0xFF00, 0xFF70))
    points |= {ord(c) for c in QUOTES} | set(range(0x2010, 0x2027))
    for a in range(0xB0, 0xD8):
        for b in range(0xA1, 0xFF):
            try:
                points.add(ord(bytes([a, b]).decode("gb2312")))
            except UnicodeDecodeError:
                pass
    return points


def save(font, points, path, features=("*",)):
    options = subset.Options()
    options.layout_features = list(features)
    options.hinting = False
    options.name_IDs = ["*"]
    options.notdef_outline = True
    subsetter = subset.Subsetter(options)
    subsetter.populate(unicodes=points)
    subsetter.subset(font)
    font.save(path)


def main(src, out):
    def pinned(name, axes):
        return instancer.instantiateVariableFont(TTFont(f"{src}/{name}.ttf"), axes)

    save(pinned("NotoSansSC", {"wght": 350}), gb2312(), f"{out}/cjk.ttf")
    save(pinned("NotoSansSC", {"wght": 350}), {ord(c) for c in QUOTES}, f"{out}/cjk-quotes.ttf", ())
    # Strong text: Markdown's bold, at the weight the panel asks for.
    save(pinned("NotoSansSC", {"wght": 600}), gb2312_level1(), f"{out}/cjk-bold.ttf")
    save(pinned("Geist", {"wght": 600}), set(TTFont(f"{src}/Geist.ttf").getBestCmap()), f"{out}/geist-bold.ttf")
    save(pinned("Inter", {"opsz": 14, "wght": 400}), {ord(c) for c in IPA}, f"{out}/phonetic.ttf")
    save(pinned("Geist", {"wght": 400}), set(TTFont(f"{src}/Geist.ttf").getBestCmap()), f"{out}/geist.ttf")
    save(pinned("GeistMono", {"wght": 400}), set(TTFont(f"{src}/GeistMono.ttf").getBestCmap()), f"{out}/geist-mono.ttf")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
