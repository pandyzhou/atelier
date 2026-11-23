"""Draws Atelier's icon, a painter's palette, to resources/icon.png (1024 px).

    python tools/icon.py

Needs Pillow. The build makes the window's, the taskbar's and the
installer's icons from that one PNG.
"""
import math
import os

from PIL import Image, ImageChops, ImageDraw, ImageFilter

S = 1024  # the size of the icon
SS = 2    # it is drawn this many times larger, then scaled down
C = 512   # its centre


def rgba(h, a=255):
    h = h.lstrip('#')
    return (int(h[0:2], 16), int(h[2:4], 16), int(h[4:6], 16), a)


# The app's colors: its accent, lighter and deeper, its danger and success
# colors, and a gold to make four dabs.
PURPLE, DEEP = rgba('#8676ff'), rgba('#5a48f0')
CORAL, TEAL, GOLD = rgba('#ff6b6b'), rgba('#4ecdc4'), rgba('#ffc857')
WHITE = rgba('#ffffff')


def blank():
    return Image.new('L', (S * SS, S * SS), 0)


def disc(d, cx, cy, r, v=255):
    d.ellipse([(cx - r) * SS, (cy - r) * SS, (cx + r) * SS, (cy + r) * SS], fill=v)


def polar(radius, degrees):
    a = math.radians(degrees)
    return C + radius * math.cos(a), C + radius * math.sin(a)


def fill(mask, color):
    layer = Image.new('RGBA', mask.size, color[:3] + (0,))
    layer.putalpha(mask.point(lambda v: v * color[3] // 255))
    return layer


def shadow(mask, dx, dy, blur, color):
    """mask's shape in color, moved and blurred."""
    moved = ImageChops.offset(mask, dx * SS, dy * SS).filter(ImageFilter.GaussianBlur(blur * SS))
    return fill(moved, color)


def gradient(mask, top, bottom):
    """mask filled from top, at its top left, to bottom, at its bottom right."""
    w, h = mask.size
    # the ramp along the diagonal, small, then smoothed up to size
    n = 64
    ramp = Image.new('L', (n, n))
    ramp.putdata([255 * (x + y) // (2 * n - 2) for y in range(n) for x in range(n)])
    ramp = ramp.resize((w, h), Image.BICUBIC)
    out = Image.composite(Image.new('RGBA', (w, h), bottom), Image.new('RGBA', (w, h), top), ramp)
    out.putalpha(mask)
    return out


def icon(inset=104, radius=200, board=296, bite=118, hole=44, dab=47, ring=186, first=153, step=50):
    """A rounded tile in the accent, and on it the palette: a disc, with a
    round bay cut at the bottom right where the hand holds it and the thumb
    hole beside that on the same diagonal; four dabs of one size stand on an
    arc around the centre, evenly apart. The whole is the same on both sides
    of that diagonal."""
    tile = blank()
    ImageDraw.Draw(tile).rounded_rectangle(
        [inset * SS, inset * SS, (S - inset) * SS, (S - inset) * SS], radius * SS, fill=255)
    img = Image.new('RGBA', (S * SS, S * SS), (0, 0, 0, 0))
    img.alpha_composite(shadow(tile, 0, 24, 30, (40, 30, 120, 90)))
    img.alpha_composite(gradient(tile, PURPLE, DEEP))

    m = blank()
    d = ImageDraw.Draw(m)
    disc(d, C, C, board)
    disc(d, *polar(board + bite * 0.18, 45), bite, 0)
    disc(d, *polar(board - bite - hole - 34, 45), hole, 0)
    # round the two points the bay leaves on the rim
    m = m.filter(ImageFilter.GaussianBlur(7 * SS)).point(lambda v: 255 if v > 127 else 0)
    m = m.filter(ImageFilter.GaussianBlur(1 * SS))
    img.alpha_composite(shadow(m, 8, 14, 14, (30, 20, 110, 120)))
    img.alpha_composite(fill(m, WHITE))

    for i, color in enumerate((DEEP, CORAL, GOLD, TEAL)):
        one = blank()
        disc(ImageDraw.Draw(one), *polar(ring, first + i * step), dab)
        img.alpha_composite(fill(one, color))
    return img.resize((S, S), Image.LANCZOS)


if __name__ == '__main__':
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    path = os.path.join(root, 'resources', 'icon.png')
    icon().save(path)
    print(path)
