"""Moxie desktop polish prototype — P0/P1 design pass.

Builds a FigCraft file with one frame per screen, using Moxie's token set
(not the kit's default SaaS palette). Run:
    python3 design/build_moxie.py
Then inspect design/renders/*.png.
"""
import os
import sys

sys.path.insert(0, "/home/mili/.config/opencode/skills/craft-suite/lib")
import figkit
from figkit import circle

BASE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(BASE, "moxie-polish.figcraft")
RENDERS = os.path.join(BASE, "renders")
os.makedirs(RENDERS, exist_ok=True)

# ---------------------------------------------------------------- Moxie tokens
# Deliberately not the kit's navy SaaS palette. These are the app's dark tokens,
# with the two small-text greys raised to clear 4.5:1 on the page ground.
TOKENS = {
    "bg": ("#121218", "#121218"),
    "surface": ("#1A1A24", "#1A1A24"),
    "surface-2": ("#22222E", "#22222E"),
    "surface-3": ("#2A2A38", "#2A2A38"),
    "border": ("#2A2A38", "#2A2A38"),
    "border-strong": ("#3C3C50", "#3C3C50"),
    "track": ("#22222E", "#22222E"),
    "bar-muted": ("#3C3C50", "#3C3C50"),
    "text": ("#E8E8F0", "#E8E8F0"),
    "text-2": ("#C4C4D2", "#C4C4D2"),
    "text-3": ("#9A9AAE", "#9A9AAE"),
    "text-4": ("#9494A8", "#9494A8"),
    "brand": ("#6A48F0", "#6A48F0"),
    "on-brand": ("#FFFFFF", "#FFFFFF"),
    "brand-tint": ("#2A2340", "#2A2340"),
    "brand-text": ("#B9A6FF", "#B9A6FF"),
    "good": ("#56C08D", "#56C08D"),
    "good-tint": ("#16281F", "#16281F"),
    "good-text": ("#7FD6AE", "#7FD6AE"),
    "warn": ("#D9A441", "#D9A441"),
    "warn-tint": ("#2A2418", "#2A2418"),
    "warn-text": ("#E8BF73", "#E8BF73"),
    "bad": ("#E06C75", "#E06C75"),
    "bad-tint": ("#2D1C20", "#2D1C20"),
    "bad-text": ("#F08A92", "#F08A92"),
    "nav-bg": ("#0C0C11", "#0C0C11"),
    "nav-active": ("#1C1C28", "#1C1C28"),
    "nav-text": ("#9A9AAE", "#9A9AAE"),
    "nav-text-active": ("#FFFFFF", "#FFFFFF"),
    "scrim": ("#00000099", "#00000099"),
    "tooltip": ("#E8E8F0", "#E8E8F0"),
    "tooltip-text": ("#121218", "#121218"),
}

ENGINE = {
    "RPGM": "#4FA3E0",
    "Ren'Py": "#B57BEE",
    "Unity": "#EA5201",
    "Godot": "#03A9F4",
    "HTML": "#8FB35F",
    "QSP": "#E06A6A",
    "WolfRPG": "#6FB873",
    "UE5": "#4A7BD0",
}

# One 2px radius everywhere; pills only for badges.
figkit.RADII.update({"sm": 2, "md": 2, "lg": 2, "pill": 999})

# Moxie scale: nothing under 11px, body 13, page title 28.
figkit.TYPE.update({
    "Display": (32, "SemiBold", "120%", "-1%"),
    "Heading/H1": (28, "SemiBold", "120%", "-1%"),
    "Heading/H2": (18, "SemiBold", "130%", "0%"),
    "Title": (15, "SemiBold", "125%", "0%"),
    "Stat": (28, "SemiBold", "120%", "-1%"),
    "Body": (13, "Regular", "150%", "0%"),
    "Body/Strong": (13, "SemiBold", "150%", "0%"),
    "Small": (12, "Regular", "140%", "0%"),
    "Label": (12, "SemiBold", "100%", "0%"),
    "Caption": (11, "Regular", "130%", "0%"),
    "Overline": (11, "SemiBold", "100%", "8%"),
})

figkit.ICONS.update({
    "library": "M4 4 H11 V20 H4 Z M13 4 H20 V20 H13 Z",
    "browse": circle(12, 12, 9) + " M3 12 H21 M12 3 C15.5 6.5 15.5 17.5 12 21 M12 3 C8.5 6.5 8.5 17.5 12 21",
    "download": "M12 3 V14 M7 9 L12 14 L17 9 M5 20 H19",
    "collections": "M12 3 L21 8 L12 13 L3 8 Z M3 12 L12 17 L21 12 M3 16 L12 21 L21 16",
    "save": "M6 3 H18 V21 L12 17 L6 21 Z",
    "scan": "M4 9 V4 H9 M20 9 V4 H15 M4 15 V20 H9 M20 15 V20 H15",
    "sync": "M20 12 C20 7.6 16.4 4 12 4 C9.6 4 7.4 5 5.8 6.6 M4 12 C4 16.4 7.6 20 12 20 C14.4 20 16.6 19 18.2 17.4 M18 4 V8 H14 M6 20 V16 H10",
    "covers": "M3 5 H21 V19 H3 Z M3 16 L8 11 L11 14 L15 10 L21 16" + circle(8, 9, 1.2),
    "duplicates": "M8 8 H20 V20 H8 Z M4 4 H16 V16",
    "trash": "M4 7 H20 M9 7 V4 H15 V7 M6 7 L7 21 H17 L18 7",
    "settings": circle(12, 12, 3) + " M12 2 V5 M12 19 V22 M2 12 H5 M19 12 H22 M5 5 L7 7 M17 17 L19 19 M19 5 L17 7 M7 17 L5 19",
    "play": "M8 5 L19 12 L8 19 Z",
    "grid": "M3 3 H10 V10 H3 Z M14 3 H21 V10 H14 Z M3 14 H10 V21 H3 Z M14 14 H21 V21 H14 Z",
    "list": "M4 6 H20 M4 12 H20 M4 18 H20",
    "plus": "M12 5 V19 M5 12 H19",
    "chevron-down": "M6 9 L12 15 L18 9",
    "arrow-left": "M19 12 H5 M11 5 L5 12 L11 19",
    "folder": "M3 6 H10 L12 8 H21 V19 H3 Z",
})


def kit():
    k = figkit.Kit(dict(name="Moxie", accent="#6A48F0", ink="#E8E8F0", font="IBM Plex Sans"), native=False)
    k.mode = "Dark"
    k.tokens.update(TOKENS)
    return k


# ---------------------------------------------------------------- atoms
def txt(k, p, s, x, y, size=13, color="text", weight="Regular", w=None, lh="150%",
        ls="0%", upper=False, mono=False, align=None, max_lines=None, name=None, opacity=None):
    props = dict(
        fontSize=max(11, size),
        fontStyle=weight,
        lineHeight=lh,
        letterSpacing=ls,
        fontFamily="IBM Plex Mono" if mono else "IBM Plex Sans",
    )
    if upper:
        props["textCase"] = "upper"
    if opacity is not None:
        props["opacity"] = opacity
    if max_lines:
        props["maxLines"] = max_lines
    if align:
        props["textAlign"] = align
    if w:
        props["width"] = w
        props["textAutoResize"] = "height"
    return k.text(p, s, "Body", color, x=x, y=y, name=name, **props)


def rect(k, p, x, y, w, h, fill=None, stroke=None, sw=1, radius=0, name="Rect", opacity=None, fills=None):
    extra = {"opacity": opacity} if opacity is not None else {}
    return k.frame(p, name, w, h, x=x, y=y, fill=fill, stroke=stroke, stroke_w=sw,
                   radius=radius, fills=fills, **extra)


def btn(k, p, x, y, w, h, label, variant="secondary", icon=None, name=None):
    spec = {
        "primary": ("brand", None, "on-brand"),
        "secondary": (None, "border-strong", "text"),
        "ghost": (None, None, "text-3"),
        "danger": (None, "bad", "bad-text"),
        "quiet": ("surface-2", None, "text-2"),
    }[variant]
    f = k.frame(p, name or f"Btn/{label}", w, h, x=x, y=y, fill=spec[0], stroke=spec[1], radius=2,
                layout="horizontal", gap=6, mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
    if icon:
        k.icon(f, icon, 14, spec[2], 1.7)
    txt(k, f, label, 0, 0, size=12, color=spec[2], weight="SemiBold", lh="100%")
    return f


def tag(k, p, x, y, label, tone="neutral", dot=None):
    bg, fg = {
        "neutral": ("surface-2", "text-2"),
        "accent": ("brand-tint", "brand-text"),
        "good": ("good-tint", "good-text"),
        "warn": ("warn-tint", "warn-text"),
        "bad": ("bad-tint", "bad-text"),
    }[tone]
    w = max(46, 18 + len(label) * 7)
    f = k.frame(p, f"Tag/{label}", w, 20, x=x, y=y, fill=bg, radius=2,
                layout="horizontal", gap=6, pad=[0, 7, 0, 7], mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
    if dot:
        k.shape(f, "ellipse", "Dot", 5, 5, x=0, y=0, fill=dot)
    txt(k, f, label, 0, 0, size=11, color=fg, weight="SemiBold", lh="100%", ls="3%")
    return f


def icon_btn(k, p, x, y, icon, size=32, color="text-3"):
    f = k.frame(p, f"IconBtn/{icon}", size, size, x=x, y=y, fill=None, stroke="border", radius=2,
                layout="horizontal", mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
    k.icon(f, icon, 15, color, 1.7)
    return f


# ---------------------------------------------------------------- sidebar
NAV = [
    (88, "Library", "library", True, None),
    (132, "Browse", "browse", False, None),
]
LIB_NAV = [
    (220, "Updates", "trend-up", False, "17"),
    (264, "Downloads", "download", False, "4"),
    (308, "Collections", "collections", False, "5"),
    (352, "Saves", "save", False, None),
]
TOOLS = [
    (440, "Scan", "scan", False, None),
    (484, "Sync", "sync", False, None),
    (528, "Covers", "covers", False, None),
    (572, "Duplicates", "duplicates", False, None),
    (616, "Trash", "trash", False, None),
]


def nav_row(k, s, y, label, icon, active=False, badge=None):
    if active:
        rect(k, s, 12, y - 8, 224, 40, fill="nav-active", radius=2, name=f"NavActive/{label}")
        rect(k, s, 0, y - 8, 2, 40, fill="brand", radius=0, name="AccentBar")
    col = "brand-text" if active else ("text-2" if badge is None else "text-2")
    k.icon(s, icon, 16, "brand-text" if active else "text-3", 1.6, x=24, y=y + 4)
    txt(k, s, label, 52, y, size=13, color="text" if active else col, weight="SemiBold" if active else "Regular", lh="100%")
    if badge:
        w = 24 if len(badge) < 3 else 30
        f = k.frame(s, f"Badge/{label}", w, 18, x=224 - w, y=y - 1, fill="brand-tint", radius=2,
                    layout="horizontal", mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
        txt(k, f, badge, 0, 0, size=10, color="brand-text", weight="SemiBold", mono=True, lh="100%")


def sidebar(k, scr, active="Library"):
    s = k.frame(scr, "Sidebar", 248, 900, x=0, y=0, fill="nav-bg")
    rect(k, s, 247, 0, 1, 900, fill="border", name="SidebarRule")
    txt(k, s, "MOXIE", 24, 22, size=19, color="text", weight="SemiBold", ls="6%", lh="100%")
    txt(k, s, "LIBRARY INDEX", 24, 46, size=11, color="text-3", weight="Medium", ls="10%", upper=True, lh="100%")

    for y, label, icon, act, badge in NAV:
        nav_row(k, s, y, label, icon, active == label, badge)

    rect(k, s, 24, 180, 200, 1, fill="border", name="Divider/A")
    txt(k, s, "LIBRARY", 24, 198, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    for y, label, icon, act, badge in LIB_NAV:
        nav_row(k, s, y, label, icon, active == label, badge)

    rect(k, s, 24, 400, 200, 1, fill="border", name="Divider/B")
    txt(k, s, "TOOLS", 24, 418, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    for y, label, icon, act, badge in TOOLS:
        nav_row(k, s, y, label, icon, active == label, badge)

    # Pinned settings + honest version.
    rect(k, s, 24, 792, 200, 1, fill="border", name="Divider/C")
    k.icon(s, "settings", 16, "brand-text" if active == "Settings" else "text-3", 1.6, x=24, y=820)
    txt(k, s, "Settings", 52, 816, size=13, color="brand-text" if active == "Settings" else "text-2", weight="SemiBold" if active == "Settings" else "Regular")
    txt(k, s, "0.4.0-alpha", 24, 862, size=11, color="text-3", mono=True, lh="100%")
    return s


# ---------------------------------------------------------------- page header
def page_header(k, scr, title, subtitle, actions=None):
    txt(k, scr, title, 280, 30, size=28, color="text", weight="SemiBold", lh="120%", ls="-1%")
    txt(k, scr, subtitle, 280, 68, size=12, color="text-3", lh="140%")
    rect(k, scr, 280, 102, 1132, 1, fill="border", name="HeaderRule")
    if actions:
        actions()


# ---------------------------------------------------------------- cover + card
def cover(k, p, x, y, w, h, c1, c2, initials, wordmark, update=False, name=None):
    f = k.frame(p, name or f"Cover/{wordmark}", w, h, x=x, y=y, radius=2, stroke="border",
                fills=[{"type": "linearGradient", "stops": [c1, c2], "from": [0, 0], "to": [1, 1]}])
    # A single soft shape gives the flat gradient some "key art" structure.
    k.shape(f, "ellipse", "Glow", int(w * 0.95), int(w * 0.95), x=int(w * 0.38), y=int(-w * 0.22),
            fill="#FFFFFF", opacity=0.045)
    scrim_h = min(96, max(24, int(h * 0.34)))
    rect(k, f, 0, h - scrim_h, w, scrim_h, name="Scrim",
         fills=[{"type": "linearGradient", "stops": ["#0B0B1200", "#0B0B12E6"], "from": [0.5, 0], "to": [0.5, 1]}])
    if h >= 90:
        ini_size = min(52, int(h * 0.42))
        txt(k, f, initials, 14, h - ini_size - 22, size=ini_size, color="#FFFFFF", weight="SemiBold",
            opacity=0.12, lh="100%", ls="-1%")
        txt(k, f, wordmark.upper(), 14, h - 34, size=11, color="#FFFFFF", weight="SemiBold", ls="10%", opacity=0.94, lh="100%")
    else:
        txt(k, f, initials, 8, 16, size=14, color="#FFFFFF", weight="SemiBold", opacity=0.5, lh="100%")
    if update:
        rect(k, f, 0, 0, 2, h, fill="brand", name="UpdateEdge")
    return f


def card(k, scr, x, y, title, engine, meta, c1, c2, initials, wordmark,
         update=False, hover=False, progress=None):
    cover(k, scr, x, y, 208, 264, c1, c2, initials, wordmark, update=update, name=f"Cover/{title}")
    if hover:
        rect(k, scr, x, y, 208, 264, fill="#00000070", name="HoverScrim")
        f = k.frame(scr, "HoverPlay", 78, 28, x=x + 65, y=y + 118, fill="brand", radius=2,
                    layout="horizontal", gap=6, mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
        k.icon(f, "play", 11, "on-brand", 1.6)
        txt(k, f, "Play", 0, 0, size=11, color="on-brand", weight="SemiBold", lh="100%")
    if progress is not None:
        rect(k, scr, x, y + 261, 208, 3, fill="surface-3", name="ProgressTrack")
        rect(k, scr, x, y + 261, int(208 * progress), 3, fill="brand", name="ProgressFill")
    # Meta line: colour dot + engine, mono version/recency right-aligned.
    k.shape(scr, "ellipse", f"Dot/{engine}", 6, 6, x=x, y=y + 280, fill=ENGINE.get(engine, "#9A9AAE"))
    txt(k, scr, engine, x + 12, y + 272, size=11, color="text-2", weight="Regular", lh="100%", ls="3%")
    meta_color = "brand-text" if update or progress is not None else "text-3"
    txt(k, scr, meta, x, y + 272, size=11, color=meta_color, mono=True, align="right", w=208, lh="100%")
    txt(k, scr, title, x, y + 292, size=13, color="text", weight="Medium", w=208, max_lines=1, lh="120%")


# ---------------------------------------------------------------- toolbar
def toolbar(k, scr):
    tabs = [("All", True, 34), ("Installed", False, 68), ("Ready to play", False, 104), ("Recently played", False, 128)]
    x = 280
    active_x = None
    for label, active, w in tabs:
        f = k.frame(scr, f"Tab/{label}", w, 40, x=x, y=122, fill=None, layout="horizontal",
                    mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
        txt(k, f, label, 0, 0, size=12, color="text" if active else "text-3",
            weight="SemiBold" if active else "Regular", lh="100%")
        if active:
            active_x = (x, w)
        x += w + 22
    if active_x:
        rect(k, scr, active_x[0], 160, active_x[1], 2, fill="brand", name="ActiveTabUnderline")

    # Search
    s = k.frame(scr, "Search", 376, 40, x=690, y=122, fill="surface", stroke="border", radius=2,
                layout="horizontal", gap=10, pad=[0, 12, 0, 12], crossAlign="center", sx="fixed", sy="fixed")
    k.icon(s, "search", 14, "text-3", 1.6)
    txt(k, s, "Search titles, tags, developers…", 0, 0, size=12, color="text-4", lh="100%")

    # Filters (count badge)
    f = k.frame(scr, "FiltersBtn", 104, 40, x=1082, y=122, fill=None, stroke="border-strong", radius=2,
                layout="horizontal", gap=7, mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
    k.icon(f, "filter", 14, "text-2", 1.6)
    txt(k, f, "Filters", 0, 0, size=12, color="text", weight="SemiBold", lh="100%")
    b = k.frame(f, "FilterCount", 20, 18, x=0, y=0, fill="brand-tint", radius=2,
                layout="horizontal", mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
    txt(k, b, "2", 0, 0, size=10, color="brand-text", weight="SemiBold", mono=True, lh="100%")

    # View (layout) button
    v = k.frame(scr, "ViewBtn", 112, 40, x=1202, y=122, fill=None, stroke="border-strong", radius=2,
                layout="horizontal", gap=7, mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
    k.icon(v, "grid", 14, "text-2", 1.6)
    txt(k, v, "Grid", 0, 0, size=12, color="text", weight="SemiBold", lh="100%")
    k.icon(v, "chevron-down", 12, "text-3", 1.6)

    icon_btn(k, scr, 1326, 126, "more", 32)


def statusbar(k, scr, left="F95Zone connected", right="17 updates ready"):
    rect(k, scr, 248, 871, 1192, 1, fill="border", name="StatusRule")
    k.shape(scr, "ellipse", "StatusDot", 6, 6, x=280, y=882, fill="good")
    txt(k, scr, left, 292, 879, size=11, color="text-3", lh="100%")
    txt(k, scr, right, 1250, 879, size=11, color="brand-text", align="right", w=162, lh="100%")


# ---------------------------------------------------------------- View popover
def view_menu(k, scr):
    x, y, w = 1140, 172, 264
    p = k.frame(scr, "ViewMenu", w, 254, x=x, y=y, fill="surface", stroke="border-strong", radius=2,
                raw_effects=[{"type": "dropShadow", "color": "#00000066", "offset": [0, 8], "radius": 24, "spread": -4}])
    txt(k, p, "LAYOUT", 16, 16, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    seg = k.frame(p, "LayoutSeg", 232, 32, x=16, y=36, fill="surface-2", radius=2,
                  layout="horizontal", gap=2, pad=2, sx="fixed", sy="fixed")
    for label, on in (("Grid", True), ("Wide", False), ("List", False)):
        f = k.frame(seg, f"Layout/{label}", 74, 28, fill="brand-tint" if on else None, radius=2,
                    layout="horizontal", mainAlign="center", crossAlign="center", sx="fill", sy="fixed")
        txt(k, f, label, 0, 0, size=11, color="brand-text" if on else "text-3",
            weight="SemiBold" if on else "Regular", lh="100%")

    rect(k, p, 16, 86, 232, 1, fill="border", name="MenuRule/A")
    txt(k, p, "DENSITY", 16, 100, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    seg2 = k.frame(p, "DensitySeg", 232, 32, x=16, y=120, fill="surface-2", radius=2,
                   layout="horizontal", gap=2, pad=2, sx="fixed", sy="fixed")
    for label, on in (("Comfortable", True), ("Compact", False)):
        f = k.frame(seg2, f"Density/{label}", 112, 28, fill="brand-tint" if on else None, radius=2,
                    layout="horizontal", mainAlign="center", crossAlign="center", sx="fill", sy="fixed")
        txt(k, f, label, 0, 0, size=11, color="brand-text" if on else "text-3",
            weight="SemiBold" if on else "Regular", lh="100%")

    rect(k, p, 16, 170, 232, 1, fill="border", name="MenuRule/B")
    txt(k, p, "CARD SIZE", 16, 184, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    rect(k, p, 16, 206, 176, 3, fill="surface-3", radius=2, name="SizeTrack")
    rect(k, p, 16, 206, 58, 3, fill="brand", radius=2, name="SizeFill")
    k.shape(p, "ellipse", "SizeKnob", 12, 12, x=68, y=202, fill="text")
    txt(k, p, "100%", 204, 198, size=11, color="text-2", mono=True, align="right", w=44, lh="100%")
    return p


def filters_menu(k, scr):
    x, y, w = 1082, 172, 300
    p = k.frame(scr, "FiltersMenu", w, 300, x=x, y=y, fill="surface", stroke="border-strong", radius=2,
                raw_effects=[{"type": "dropShadow", "color": "#00000066", "offset": [0, 8], "radius": 24, "spread": -4}])
    txt(k, p, "STATUS", 16, 16, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    tag(k, p, 16, 36, "All", "accent")
    tag(k, p, 64, 36, "Active", "neutral")
    tag(k, p, 126, 36, "Completed", "neutral")
    tag(k, p, 16, 64, "Abandoned", "neutral")
    tag(k, p, 106, 64, "On hold", "neutral")
    tag(k, p, 174, 64, "Unknown", "neutral")

    rect(k, p, 16, 104, 268, 1, fill="border", name="FilterRule/A")
    txt(k, p, "ENGINE", 16, 118, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    sel = k.frame(p, "EngineSelect", 268, 36, x=16, y=138, fill="surface-2", stroke="border", radius=2,
                  layout="horizontal", pad=[0, 10, 0, 10], crossAlign="center", sx="fixed", sy="fixed")
    txt(k, sel, "All engines", 0, 0, size=12, color="text-2", lh="100%")
    k.icon(sel, "chevron-down", 12, "text-3", 1.6)

    txt(k, p, "SORT", 16, 190, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    sel2 = k.frame(p, "SortSelect", 268, 36, x=16, y=210, fill="surface-2", stroke="border", radius=2,
                   layout="horizontal", pad=[0, 10, 0, 10], crossAlign="center", sx="fixed", sy="fixed")
    txt(k, sel2, "Recently played", 0, 0, size=12, color="text-2", lh="100%")
    k.icon(sel2, "chevron-down", 12, "text-3", 1.6)

    rect(k, p, 16, 262, 268, 1, fill="border", name="FilterRule/B")
    txt(k, p, "Clear filters", 16, 274, size=12, color="text-3", weight="SemiBold", lh="100%")
    return p


# ---------------------------------------------------------------- 01 library
def library(k, name="01 Library", x=0, view_open=False, filters_open=False):
    scr = k.screen(name, 1440, 900, x=x, y=0, mode="Dark")
    sidebar(k, scr, "Library")
    page_header(k, scr, "Library", "81 games · 17 updates · synced 2h ago",
                lambda: btn(k, scr, 1296, 30, 116, 36, "Scan folder", "secondary", icon="plus"))
    toolbar(k, scr)

    cards = [
        ("Signal Academy", "RPGM", "? → 0.4.0", "#3B2A6E", "#171326", "SA", "Signal", True, True, None),
        ("Cinder Season", "Ren'Py", "Updating · 62%", "#263A8F", "#10162E", "CS", "Cinder", False, False, 0.62),
        ("Bramble Diaries", "Ren'Py", "0.8.0 → 0.9.0", "#1D5A63", "#0D2226", "BD", "Bramble", True, False, None),
        ("Winter Academy", "QSP", "? → 0.8.0", "#23324D", "#10151F", "WA", "Winter", False, False, None),
        ("Cinder Season", "Godot", "1mo ago", "#6A2A24", "#24100E", "CS", "Cinder", False, False, None),
        ("Summer Diaries", "RPGM", "1mo ago", "#4A3B17", "#1C1608", "SD", "Summer", False, False, None),
        ("Winter Academy", "Ren'Py", "? → 0.2.0", "#1F4A34", "#0D1C15", "WA", "Winter", True, False, None),
        ("Quiet Season", "Ren'Py", "0.9.0 → 1.0.0", "#5A1F4A", "#200A1A", "QS", "Quiet", True, False, None),
        ("Summer Diaries", "QSP", "1mo ago", "#1D3F6E", "#0C1828", "SD", "Summer", False, False, None),
        ("Velvet Academy [Ch. 2]", "Godot", "? → 0.6.0", "#4A2A78", "#180E28", "VA", "Velvet", False, False, None),
    ]
    for i, (title, engine, meta, c1, c2, ini, word, upd, hov, prog) in enumerate(cards):
        x = 280 + (i % 5) * 228
        y = 196 + (i // 5) * 340
        card(k, scr, x, y, title, engine, meta, c1, c2, ini, word, upd, hov, prog)

    statusbar(k, scr)
    if view_open:
        view_menu(k, scr)
    if filters_open:
        filters_menu(k, scr)
    return scr


# ---------------------------------------------------------------- 04 detail
def detail(k, name="04 Detail", x=0):
    scr = k.screen(name, 1440, 900, x=x, y=0, mode="Dark")
    sidebar(k, scr, "Library")

    f = k.frame(scr, "Back", 150, 24, x=280, y=22, fill=None, layout="horizontal", gap=7, crossAlign="center", sx="fixed", sy="fixed")
    k.icon(f, "arrow-left", 13, "text-3", 1.6)
    txt(k, f, "Back to library", 0, 0, size=12, color="text-3", lh="100%")

    txt(k, scr, "Signal Academy", 280, 48, size=32, color="text", weight="SemiBold", lh="110%", ls="-1%")
    tag(k, scr, 280, 94, "Installed: unknown", "warn")
    tag(k, scr, 434, 94, "RPGM", "neutral")
    tag(k, scr, 490, 94, "Active", "good")

    # Play bar: one anchor, update as its secondary action.
    bar = k.frame(scr, "PlayBar", 1132, 72, x=280, y=140, fill="surface", stroke="border", radius=2)
    btn(k, bar, 16, 14, 132, 44, "Play", "primary", icon="play", name="Play")
    rect(k, bar, 166, 14, 1, 44, fill="border", name="PlayDivider")
    txt(k, bar, "Update available", 186, 14, size=13, color="warn-text", weight="SemiBold", lh="110%")
    txt(k, bar, "Installed version unknown · latest 0.4.0", 186, 34, size=11, color="text-3", lh="100%")
    btn(k, bar, 410, 18, 154, 36, "Download update", "secondary", name="DownloadUpdate")
    txt(k, bar, "Last played 2 days ago · 6.2h played", 600, 34, size=11, color="text-3", lh="100%")
    btn(k, bar, 900, 18, 132, 36, "Mark as current", "ghost", name="MarkCurrent")
    icon_btn(k, bar, 1050, 18, "more", 36)

    # Cover + management column.
    cover(k, scr, 280, 238, 320, 400, "#3B2A6E", "#171326", "SA", "Signal Academy", update=True, name="DetailCover")
    btn(k, scr, 280, 650, 160, 34, "Choose cover…", "secondary", name="ChooseCover")
    btn(k, scr, 448, 650, 72, 34, "Lock", "secondary", name="Lock")
    btn(k, scr, 280, 700, 320, 36, "Sync from F95Zone", "secondary", icon="sync", name="SyncFromF95")
    btn(k, scr, 280, 744, 320, 36, "Edit", "secondary", name="EditGame")
    btn(k, scr, 280, 788, 320, 36, "Rename", "secondary", name="RenameGame")
    btn(k, scr, 280, 832, 320, 36, "Remove game", "danger", name="RemoveGame")

    # Details record.
    txt(k, scr, "DETAILS", 632, 238, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    rows = [
        ("Developer", "Generated Studio"),
        ("Engine", "RPGM"),
        ("Version", "unknown → 0.4.0"),
        ("Status", "Active"),
        ("Size", "5.9 GB"),
        ("Path", "/games/Signal_Academy"),
    ]
    for i, (label, value) in enumerate(rows):
        y = 264 + i * 36
        if i:
            rect(k, scr, 632, y - 1, 780, 1, fill="border", name=f"MetaRule/{label}")
        txt(k, scr, label.upper(), 632, y + 9, size=11, color="text-3", weight="Medium", ls="8%", lh="100%")
        color = "brand-text" if label == "Version" else "text"
        mono = label in ("Size", "Path", "Version")
        txt(k, scr, value, 788, y + 7, size=13, color=color, mono=mono, lh="120%")

    # Collections row
    y = 264 + len(rows) * 36
    rect(k, scr, 632, y - 1, 780, 1, fill="border", name="MetaRule/Collections")
    txt(k, scr, "COLLECTIONS", 632, y + 9, size=11, color="text-3", weight="Medium", ls="8%", lh="100%")
    c1 = k.frame(scr, "Collection/Visual Novels", 116, 22, x=788, y=y + 3, fill="brand-tint", radius=2,
                 layout="horizontal", gap=6, pad=[0, 7, 0, 7], mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
    txt(k, c1, "Visual Novels", 0, 0, size=10, color="brand-text", weight="SemiBold", lh="100%")
    k.icon(c1, "x", 9, "brand-text", 1.6)
    c2 = k.frame(scr, "Collection/Finished", 82, 22, x=912, y=y + 3, fill="surface-2", radius=2,
                 layout="horizontal", gap=6, pad=[0, 7, 0, 7], mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
    txt(k, c2, "Finished", 0, 0, size=10, color="text-2", weight="SemiBold", lh="100%")
    k.icon(c2, "x", 9, "text-3", 1.6)
    txt(k, scr, "+ Add to collection", 1004, y + 6, size=11, color="text-3", lh="100%")

    # Advanced disclosure
    y2 = y + 44
    rect(k, scr, 632, y2 - 1, 780, 1, fill="border", name="MetaRule/Advanced")
    txt(k, scr, "Advanced", 632, y2 + 9, size=11, color="text-3", weight="Medium", ls="8%", upper=True, lh="100%")
    txt(k, scr, "Executable, Wine prefix, tags", 788, y2 + 8, size=12, color="text-3", lh="100%")
    txt(k, scr, "Show", 1360, y2 + 8, size=11, color="brand-text", weight="SemiBold", align="right", w=52, lh="100%")

    # Overview
    y3 = y2 + 56
    txt(k, scr, "Overview", 632, y3, size=18, color="text", weight="SemiBold", lh="120%")
    txt(k, scr, "Signal Academy is an RPGM game about choices, relationships, and the consequences that follow. The story branches based on the decisions you make, with multiple endings and a large cast of characters.",
        632, y3 + 30, size=13, color="text-2", w=600, lh="160%")

    # Notes
    y4 = y3 + 118
    txt(k, scr, "Notes", 632, y4, size=18, color="text", weight="SemiBold", lh="120%")
    note = k.frame(scr, "NotesEmpty", 780, 54, x=632, y=y4 + 30, fill=None, stroke="border", radius=2,
                   layout="horizontal", pad=[0, 12, 0, 12], crossAlign="center", sx="fixed", sy="fixed")
    txt(k, note, "No notes yet.", 0, 0, size=12, color="text-3", lh="100%")
    txt(k, scr, "Add note", 1360, y4 + 45, size=12, color="brand-text", weight="SemiBold", align="right", w=52, lh="100%")

    # Download links collapsed row
    y5 = y4 + 104
    rect(k, scr, 632, y5 - 1, 780, 1, fill="border", name="MetaRule/Downloads")
    txt(k, scr, "Download links", 632, y5 + 10, size=13, color="text", weight="SemiBold", lh="100%")
    tag(k, scr, 748, y5 + 7, "2", "neutral")
    txt(k, scr, "Show", 1360, y5 + 10, size=11, color="brand-text", weight="SemiBold", align="right", w=52, lh="100%")
    return scr


# ---------------------------------------------------------------- 05 covers
def covers(k, name="05 Cover art", x=0):
    scr = k.screen(name, 1440, 900, x=x, y=0, mode="Dark")
    sidebar(k, scr, "Covers")

    def actions():
        btn(k, scr, 1012, 30, 160, 36, "Upgrade to portrait", "secondary", name="UpgradePortrait")
        btn(k, scr, 1188, 30, 200, 36, "Fetch missing covers", "primary", icon="download", name="FetchCovers")

    page_header(k, scr, "Cover art", "68 of 81 games have covers · 13 missing", actions)

    strip = k.frame(scr, "Coverage", 1132, 88, x=280, y=128, fill="surface", stroke="border", radius=2)
    txt(k, strip, "68 / 81", 20, 16, size=26, color="text", weight="SemiBold", lh="100%")
    txt(k, strip, "covers cached", 20, 56, size=11, color="text-3", lh="100%")
    rect(k, strip, 200, 38, 460, 8, fill="surface-3", radius=4, name="CoverageTrack")
    rect(k, strip, 200, 38, 386, 8, fill="brand", radius=4, name="CoverageFill")
    txt(k, strip, "84%", 676, 32, size=12, color="text-2", mono=True, lh="100%")
    tag(k, strip, 840, 32, "13 missing", "warn")
    txt(k, strip, "Last scan 2h ago", 1000, 32, size=11, color="text-3", align="right", w=112, lh="100%")

    txt(k, scr, "MISSING COVERS", 280, 244, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    tag(k, scr, 414, 238, "13", "neutral")
    btn(k, scr, 1000, 270, 120, 34, "View all 13", "secondary", name="ViewAll")

    missing = [
        ("Velvet Academy [Ch. 2]", "F95Zone", "VA"),
        ("Quiet Season", "Steam", "QS"),
        ("Moth Diaries", "SteamGridDB", "MD"),
        ("Glass Garden", "VNDB", "GG"),
    ]
    for i, (title, source, ini) in enumerate(missing):
        x0 = 280 + i * 176
        f = k.frame(scr, f"MissingCover/{title}", 160, 214, x=x0, y=270, fill="#15151D", stroke="border-strong",
                    radius=2, dashes=[4, 4])
        k.icon(f, "covers", 22, "text-3", 1.4, x=69, y=80)
        txt(k, f, ini, 16, 142, size=34, color="text-3", weight="SemiBold", opacity=0.55, lh="100%")
        txt(k, f, "No cover", 16, 186, size=11, color="text-3", lh="100%")
        txt(k, scr, title, x0, 494, size=11, color="text-2", weight="Medium", w=160, max_lines=1, lh="120%")
        txt(k, scr, source, x0, 512, size=11, color="text-3", lh="100%")

    # Sources panel
    p = k.frame(scr, "SourcesPanel", 412, 260, x=1000, y=320, fill="surface", stroke="border", radius=2)
    txt(k, p, "SOURCES", 20, 18, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    sources = [
        ("F95Zone", "thread images · cookie-free", "On", "good"),
        ("Steam", "app header art", "On", "good"),
        ("SteamGridDB", "portrait key art", "Key needed", "warn"),
        ("VNDB", "portrait covers", "Off", "neutral"),
    ]
    for j, (src, note, state, tone) in enumerate(sources):
        yy = 44 + j * 42
        k.shape(p, "ellipse", f"SourceDot/{src}", 6, 6, x=20, y=yy + 7, fill="good" if tone == "good" else ("warn" if tone == "warn" else "text-3"))
        txt(k, p, src, 34, yy, size=12, color="text", weight="Medium", lh="100%")
        txt(k, p, note, 34, yy + 16, size=10, color="text-3", lh="100%")
        txt(k, p, state, 312, yy + 4, size=11, color="good-text" if tone == "good" else ("warn-text" if tone == "warn" else "text-3"),
            align="right", w=80, lh="100%")
    rect(k, p, 20, 216, 372, 1, fill="border", name="SourcesRule")
    txt(k, p, "Exact title match · portrait ≥ 600px · locked covers are skipped", 20, 228, size=10, color="text-3", lh="100%")

    # Recent activity
    txt(k, scr, "RECENT ACTIVITY", 280, 608, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    rect(k, scr, 280, 636, 1132, 1, fill="border", name="ActivityHead")
    txt(k, scr, "WHEN", 280, 646, size=10, color="text-3", weight="SemiBold", ls="8%", lh="100%")
    txt(k, scr, "RUN", 420, 646, size=10, color="text-3", weight="SemiBold", ls="8%", lh="100%")
    txt(k, scr, "RESULT", 1200, 646, size=10, color="text-3", weight="SemiBold", ls="8%", align="right", w=212, lh="100%")
    activity = [
        ("2h ago", "Fetched missing covers", "13 updated", "good"),
        ("Yesterday", "Upgraded to portrait art", "8 replaced", "accent"),
        ("3d ago", "Fetched missing covers", "4 skipped · locked", "text-3"),
    ]
    for j, (when, run, result, tone) in enumerate(activity):
        yy = 676 + j * 38
        rect(k, scr, 280, yy - 1, 1132, 1, fill="border", name=f"ActivityRule/{j}")
        txt(k, scr, when, 280, yy + 10, size=11, color="text-3", mono=True, lh="100%")
        txt(k, scr, run, 420, yy + 10, size=12, color="text", lh="100%")
        txt(k, scr, result, 1200, yy + 10, size=11, color="good-text" if tone == "good" else ("brand-text" if tone == "accent" else "text-3"),
            align="right", w=212, lh="100%")

    txt(k, scr, "Covers are cached locally; the library never blocks on a fetch.", 280, 820, size=11, color="text-3", lh="100%")
    return scr


# ---------------------------------------------------------------- 06 sync
def sync_screen(k, name="06 Sync", x=0):
    scr = k.screen(name, 1440, 900, x=x, y=0, mode="Dark")
    sidebar(k, scr, "Sync")

    def actions():
        cb = k.frame(scr, "FullSyncCheck", 16, 16, x=1040, y=40, fill="surface", stroke="border-strong", radius=2)
        txt(k, scr, "Full sync", 1064, 38, size=12, color="text-2", lh="100%")
        txt(k, scr, "ignores the 24h cooldown", 1064, 56, size=11, color="text-3", lh="100%")
        btn(k, scr, 1272, 30, 140, 36, "Start sync", "primary", icon="sync", name="StartSync")

    page_header(k, scr, "F95Zone sync", "Auto-associate games and check for updates.", actions)

    stats = [
        ("COOKIES", "OK", "F95Zone session available", "good"),
        ("LAST SYNC", "2h ago", "0 failures", "text"),
        ("UPDATES", "17 ready", "across 12 games", "warn"),
    ]
    for i, (label, value, sub, tone) in enumerate(stats):
        x0 = 280 + i * 380
        c = k.frame(scr, f"Stat/{label}", 372, 84, x=x0, y=128, fill="surface", stroke="border", radius=2)
        txt(k, c, label, 18, 16, size=11, color="text-3", weight="SemiBold", ls="10%", lh="100%")
        txt(k, c, value, 18, 36, size=22, color=("good-text" if tone == "good" else ("warn-text" if tone == "warn" else "text")),
            weight="SemiBold", lh="110%")
        txt(k, c, sub, 18, 64, size=11, color="text-3", lh="100%")

    plan = k.frame(scr, "Plan", 1132, 196, x=280, y=236, fill="surface", stroke="border", radius=2)
    txt(k, plan, "What will run", 20, 18, size=18, color="text", weight="SemiBold", lh="120%")
    txt(k, plan, "3 steps · about 4 minutes", 20, 44, size=11, color="text-3", lh="100%")
    rows = [
        ("81 games checked for new versions", "81", "text-3"),
        ("17 updates downloaded and merged", "17", "brand-text"),
        ("12 unlinked games matched to F95Zone threads", "12", "text-3"),
    ]
    for j, (text, count, tone) in enumerate(rows):
        yy = 78 + j * 36
        if j:
            rect(k, plan, 20, yy - 6, 1092, 1, fill="border", name=f"PlanRule/{j}")
        k.icon(plan, "check", 14, "good", 1.8, x=20, y=yy + 2)
        txt(k, plan, text, 44, yy, size=13, color="text-2", lh="120%")
        txt(k, plan, count, 1040, yy, size=12, color=tone, mono=True, align="right", w=72, lh="120%")

    txt(k, scr, "RECENT RUNS", 280, 464, size=11, color="text-3", weight="SemiBold", ls="10%", upper=True, lh="100%")
    rect(k, scr, 280, 492, 1132, 1, fill="border", name="RunsHead")
    for label, xx, ww in [("WHEN", 280, 120), ("RUN", 440, 180), ("RESULT", 720, 220), ("DURATION", 1080, 100)]:
        txt(k, scr, label, xx, 502, size=11, color="text-3", weight="SemiBold", ls="8%", lh="100%")
    txt(k, scr, "STATUS", 1250, 502, size=11, color="text-3", weight="SemiBold", ls="8%", align="right", w=162, lh="100%")

    runs = [
        ("Today 09:14", "Full sync", "17 updates", "4m 12s", "Complete"),
        ("Yesterday", "Quick check", "No updates", "1m 40s", "Complete"),
        ("2d ago", "Quick check", "3 updates", "2m 06s", "Complete"),
    ]
    for j, (when, kind, result, dur, status) in enumerate(runs):
        yy = 532 + j * 40
        rect(k, scr, 280, yy - 1, 1132, 1, fill="border", name=f"RunRule/{j}")
        txt(k, scr, when, 280, yy + 10, size=11, color="text-3", mono=True, lh="100%")
        txt(k, scr, kind, 440, yy + 10, size=12, color="text", lh="100%")
        txt(k, scr, result, 720, yy + 10, size=12, color="text-2", lh="100%")
        txt(k, scr, dur, 1080, yy + 10, size=11, color="text-3", mono=True, lh="100%")
        txt(k, scr, status, 1250, yy + 10, size=11, color="good-text", align="right", w=162, lh="100%")

    polite = k.frame(scr, "PoliteCard", 1132, 100, x=280, y=680, fill="surface", stroke="border", radius=2)
    txt(k, polite, "How sync stays polite", 20, 16, size=16, color="text", weight="SemiBold", lh="120%")
    for j, text in enumerate(["One request at a time", "24h cooldown between full syncs", "Browser only for security checks"]):
        x0 = 20 + j * 360
        k.icon(polite, "check", 12, "good", 1.8, x=x0, y=56)
        txt(k, polite, text, x0 + 20, 52, size=11, color="text-2", lh="100%")

    txt(k, scr, "Sync runs in the background; you can keep browsing and launching games.", 280, 820, size=11, color="text-3", lh="100%")
    return scr


# ---------------------------------------------------------------- 07 add game
def add_game(k, name="07 Add game", x=0):
    scr = k.screen(name, 1440, 900, x=x, y=0, mode="Dark")
    sidebar(k, scr, "Add Game")
    page_header(k, scr, "Add game", "Point Moxie at a folder. It detects the engine and adds the title.")

    card = k.frame(scr, "AddForm", 640, 390, x=280, y=128, fill="surface", stroke="border", radius=2)
    txt(k, card, "GAME FOLDER", 20, 20, size=11, color="text-3", weight="SemiBold", ls="10%", lh="100%")
    inp = k.frame(card, "PathInput", 498, 44, x=20, y=42, fill="surface-2", stroke="border", radius=2,
                  layout="horizontal", pad=[0, 12, 0, 12], crossAlign="center", sx="fixed", sy="fixed")
    txt(k, inp, "/home/mili/Games/Signal_Academy", 0, 0, size=12, color="text-2", mono=True, lh="100%")
    btn(k, card, 528, 42, 92, 44, "Browse", "secondary", icon="folder", name="BrowseFolder")
    btn(k, card, 20, 104, 136, 38, "Detect engine", "secondary", icon="scan", name="DetectEngine")
    txt(k, card, "Runs automatically when the folder exists", 168, 114, size=11, color="text-3", lh="100%")
    rect(k, card, 20, 160, 600, 1, fill="border", name="FormRule")

    txt(k, card, "DETECTED", 20, 180, size=11, color="text-3", weight="SemiBold", ls="10%", lh="100%")
    cover(k, card, 20, 204, 80, 104, "#3B2A6E", "#171326", "SA", "Signal", update=True, name="AddCover")
    txt(k, card, "Signal Academy", 118, 204, size=16, color="text", weight="SemiBold", lh="120%")
    k.shape(card, "ellipse", "AddEngineDot", 5, 5, x=119, y=235, fill=ENGINE["RPGM"])
    txt(k, card, "RPGM", 130, 230, size=11, color="text-2", lh="100%")
    tag(k, card, 178, 227, "Detected", "good")
    txt(k, card, "/home/mili/Games/Signal_Academy", 118, 250, size=11, color="text-3", mono=True, lh="100%")
    txt(k, card, "1.4.2 · 2.6 GB", 118, 272, size=11, color="text-3", mono=True, lh="100%")
    btn(k, card, 20, 330, 600, 42, "Add to library", "primary", name="AddToLibrary")

    # Right rail: engines + next steps.
    rail = k.frame(scr, "AddRail", 452, 390, x=960, y=128, fill="surface", stroke="border", radius=2)
    txt(k, rail, "SUPPORTED ENGINES", 20, 20, size=11, color="text-3", weight="SemiBold", ls="10%", lh="100%")
    engines = ["RPGM", "Ren'Py", "Unity", "Godot", "HTML", "QSP", "WolfRPG", "UE5"]
    ex, ey = 20, 40
    for e in engines:
        tw = max(46, 18 + len(e) * 7)
        if ex + tw > 420:
            ex, ey = 20, ey + 30
        tag(k, rail, ex, ey, e, "neutral")
        ex += tw + 8
    rect(k, rail, 20, 132, 412, 1, fill="border", name="RailRule")
    txt(k, rail, "WHAT HAPPENS NEXT", 20, 148, size=11, color="text-3", weight="SemiBold", ls="10%", lh="100%")
    steps = [
        ("1", "Moxie reads the folder and identifies the engine."),
        ("2", "It finds the executable and estimates the install size."),
        ("3", "The game appears in Library; add metadata from F95Zone."),
    ]
    for j, (num, text) in enumerate(steps):
        yy = 174 + j * 48
        c = k.frame(rail, f"Step/{num}", 24, 24, x=20, y=yy, fill="brand-tint", radius=12,
                    layout="horizontal", mainAlign="center", crossAlign="center", sx="fixed", sy="fixed")
        txt(k, c, num, 0, 0, size=11, color="brand-text", weight="SemiBold", mono=True, lh="100%")
        txt(k, rail, text, 56, yy, size=12, color="text-2", w=360, lh="140%")
    txt(k, rail, "The folder must contain the game executable or a recognizable engine signature.", 20, 334, size=11, color="text-3", w=412, lh="140%")

    # Bulk path below the form.
    bulk = k.frame(scr, "BulkCard", 1132, 150, x=280, y=548, fill="surface", stroke="border", radius=2)
    txt(k, bulk, "Adding a folder of games?", 20, 18, size=18, color="text", weight="SemiBold", lh="120%")
    txt(k, bulk, "Scan a directory tree, detect every engine and add everything at once.", 20, 48, size=12, color="text-3", lh="100%")
    btn(k, bulk, 992, 22, 120, 36, "Open Scan", "secondary", icon="scan", name="OpenScan")
    for j, (name_, count) in enumerate([("RPGM", "24 games"), ("Ren'Py", "31 games"), ("Unity", "9 games"), ("Other", "17 games")]):
        x0 = 20 + j * 272
        k.shape(bulk, "ellipse", f"BulkDot/{name_}", 6, 6, x=x0, y=96, fill=ENGINE.get(name_, "#9A9AAE"))
        txt(k, bulk, name_, x0 + 12, 90, size=12, color="text-2", weight="Medium", lh="100%")
        txt(k, bulk, count, x0 + 12, 108, size=11, color="text-3", mono=True, lh="100%")
    txt(k, scr, "You can also drag a folder onto the window.", 280, 720, size=11, color="text-3", lh="100%")

    recent = k.frame(scr, "RecentCard", 1132, 130, x=280, y=740, fill="surface", stroke="border", radius=2)
    txt(k, recent, "RECENTLY ADDED", 20, 16, size=11, color="text-3", weight="SemiBold", ls="10%", lh="100%")
    for j, (title, path, when) in enumerate([
        ("Signal Academy", "/games/Signal_Academy", "2d ago"),
        ("Quiet Season", "/games/Quiet_Season", "4d ago"),
        ("Velvet Academy", "/games/Velvet_Academy", "1w ago"),
    ]):
        yy = 42 + j * 28
        rect(k, recent, 20, yy - 6, 1092, 1, fill="border", name=f"RecentRule/{j}")
        txt(k, recent, title, 20, yy, size=12, color="text", weight="Medium", lh="100%")
        txt(k, recent, path, 300, yy, size=11, color="text-3", mono=True, lh="100%")
        txt(k, recent, when, 950, yy, size=11, color="text-3", align="right", w=162, lh="100%")
    return scr


# ---------------------------------------------------------------- 08 settings
def settings(k, name="08 Settings", x=0):
    scr = k.screen(name, 1440, 900, x=x, y=0, mode="Dark")
    sidebar(k, scr, "Settings")
    page_header(k, scr, "Settings", "Scan paths, dependencies, updates and where Moxie keeps data.")

    nav = k.frame(scr, "SettingsNav", 232, 312, x=280, y=128, fill="surface", stroke="border", radius=2)
    items = ["Scan paths", "Install", "Dependencies", "Updates", "Cover art", "Appearance", "About"]
    for j, item in enumerate(items):
        yy = 12 + j * 40
        on = item == "Scan paths"
        if on:
            rect(k, nav, 8, yy, 216, 34, fill="surface-2", radius=2, name="SettingsActive")
            rect(k, nav, 8, yy, 2, 34, fill="brand", name="SettingsBar")
        txt(k, nav, item, 24, yy + 9, size=12, color="brand-text" if on else "text-2",
            weight="SemiBold" if on else "Regular", lh="100%")

    # Scan paths
    txt(k, scr, "Scan paths", 544, 128, size=18, color="text", weight="SemiBold", lh="120%")
    txt(k, scr, "Directories watched for new and removed games. Changes take effect immediately.", 544, 156, size=11, color="text-3", lh="100%")
    inp = k.frame(scr, "SettingsPathInput", 700, 44, x=544, y=190, fill="surface-2", stroke="border", radius=2,
                  layout="horizontal", pad=[0, 12, 0, 12], crossAlign="center", sx="fixed", sy="fixed")
    txt(k, inp, "/path/to/games", 0, 0, size=12, color="text-3", mono=True, lh="100%")
    btn(k, scr, 1254, 190, 120, 44, "Add path", "primary", name="AddPath")
    for j, path in enumerate(["/home/mili/Games", "/mnt/games/archive"]):
        yy = 248 + j * 44
        row = k.frame(scr, f"PathRow/{j}", 830, 38, x=544, y=yy, fill="surface", stroke="border", radius=2)
        k.icon(row, "folder", 14, "text-3", 1.6, x=12, y=12)
        txt(k, row, path, 36, 12, size=11, color="text-2", mono=True, lh="100%")
        btn(k, row, 676, 5, 64, 28, "Scan", "secondary", name=f"ScanPath/{j}")
        k.icon(row, "x", 12, "text-3", 1.6, x=760, y=13)

    rect(k, scr, 544, 360, 868, 1, fill="border", name="SettingsRule/A")

    # Two columns: install + updates
    txt(k, scr, "Install location", 544, 384, size=18, color="text", weight="SemiBold", lh="120%")
    txt(k, scr, "Where downloads are installed. The engine can add a subfolder automatically.", 544, 412, size=11, color="text-3", w=400, lh="140%")
    cb = k.frame(scr, "EngineSubfolderCheck", 16, 16, x=544, y=470, fill="surface-2", stroke="border-strong", radius=2)
    k.icon(cb, "check", 10, "brand-text", 1.8)
    txt(k, scr, "Organize installs by engine", 570, 468, size=12, color="text-2", lh="100%")

    txt(k, scr, "Game updates", 992, 384, size=18, color="text", weight="SemiBold", lh="120%")
    txt(k, scr, "How many updates download and install at once.", 992, 412, size=11, color="text-3", w=400, lh="140%")
    seg = k.frame(scr, "ParallelSeg", 184, 34, x=992, y=462, fill="surface-2", radius=2,
                  layout="horizontal", gap=2, pad=2, sx="fixed", sy="fixed")
    for n in ("1", "2", "3", "4"):
        on = n == "2"
        f = k.frame(seg, f"Parallel/{n}", 44, 30, fill="brand-tint" if on else None, radius=2,
                    layout="horizontal", mainAlign="center", crossAlign="center", sx="fill", sy="fixed")
        txt(k, f, n, 0, 0, size=12, color="brand-text" if on else "text-3", weight="SemiBold", mono=True, lh="100%")

    rect(k, scr, 544, 528, 868, 1, fill="border", name="SettingsRule/B")

    # Two columns: cover art + appearance
    txt(k, scr, "Cover art", 544, 552, size=18, color="text", weight="SemiBold", lh="120%")
    txt(k, scr, "Upgrade missing, landscape or low-resolution covers to portrait key art.", 544, 580, size=11, color="text-3", w=400, lh="140%")
    btn(k, scr, 544, 620, 170, 36, "Upgrade to portrait", "secondary", name="SettingsUpgrade")

    txt(k, scr, "Appearance", 992, 552, size=18, color="text", weight="SemiBold", lh="120%")
    txt(k, scr, "Theme and density. Both are remembered per window.", 992, 580, size=11, color="text-3", w=400, lh="140%")
    sel = k.frame(scr, "ThemeSelect", 184, 36, x=992, y=620, fill="surface-2", stroke="border", radius=2,
                  layout="horizontal", pad=[0, 10, 0, 10], crossAlign="center", sx="fixed", sy="fixed")
    txt(k, sel, "Dark", 0, 0, size=12, color="text-2", lh="100%")
    k.icon(sel, "chevron-down", 12, "text-3", 1.6)

    rect(k, scr, 544, 684, 868, 1, fill="border", name="SettingsRule/C")
    txt(k, scr, "Data folder", 544, 704, size=18, color="text", weight="SemiBold", lh="120%")
    txt(k, scr, "The SQLite database, cached covers and save snapshots live here.", 544, 732, size=11, color="text-3", lh="100%")
    btn(k, scr, 1280, 704, 132, 36, "Open folder", "secondary", icon="folder", name="OpenData")
    txt(k, scr, "Moxie 0.4.0-alpha · desktop", 544, 840, size=11, color="text-3", lh="100%")
    return scr


# ---------------------------------------------------------------- 09 trash
def trash(k, name="09 Trash", x=0):
    scr = k.screen(name, 1440, 900, x=x, y=0, mode="Dark")
    sidebar(k, scr, "Trash")

    def actions():
        btn(k, scr, 1188, 30, 132, 36, "Restore all", "secondary", name="RestoreAll")
        icon_btn(k, scr, 1336, 32, "more", 32)

    page_header(k, scr, "Trash", "1 game · removed 2 days ago", actions)
    rect(k, scr, 280, 128, 1132, 1, fill="border", name="TrashHead")
    txt(k, scr, "GAME", 280, 138, size=11, color="text-3", weight="SemiBold", ls="8%", lh="100%")
    txt(k, scr, "ENGINE", 720, 138, size=11, color="text-3", weight="SemiBold", ls="8%", lh="100%")
    txt(k, scr, "REMOVED", 900, 138, size=11, color="text-3", weight="SemiBold", ls="8%", lh="100%")
    txt(k, scr, "ACTIONS", 1250, 138, size=11, color="text-3", weight="SemiBold", ls="8%", align="right", w=162, lh="100%")

    rect(k, scr, 280, 168, 1132, 72, fill="surface", stroke="border", radius=2, name="TrashRowBg")
    cover(k, scr, 292, 178, 40, 52, "#23324D", "#10151F", "OP", "Old", name="TrashCover")
    txt(k, scr, "Old Project X", 344, 180, size=13, color="text", weight="SemiBold", lh="100%")
    txt(k, scr, "/games/Old_Project_X", 344, 200, size=11, color="text-3", mono=True, lh="100%")
    k.shape(scr, "ellipse", "TrashEngineDot", 5, 5, x=721, y=194, fill=ENGINE["Unity"])
    txt(k, scr, "Unity", 732, 189, size=12, color="text-2", lh="100%")
    txt(k, scr, "2 days ago", 900, 191, size=12, color="text-3", lh="100%")
    btn(k, scr, 1178, 184, 92, 32, "Restore", "primary", name="RestoreGame")
    icon_btn(k, scr, 1286, 184, "more", 32)

    help_card = k.frame(scr, "TrashHelp", 1132, 156, x=280, y=272, fill="surface", stroke="border", radius=2)
    txt(k, help_card, "What emptying Trash removes", 20, 18, size=18, color="text", weight="SemiBold", lh="120%")
    bullets = [
        ("The game folder on disk", "bad"),
        ("Metadata, tags and cached covers", "bad"),
        ("Save snapshots and play history are kept", "good"),
    ]
    for j, (text, tone) in enumerate(bullets):
        yy = 56 + j * 26
        k.icon(help_card, "check" if tone == "good" else "x", 12, "good" if tone == "good" else "bad-text", 1.8, x=20, y=yy + 2)
        txt(k, help_card, text, 40, yy, size=12, color="text-2", lh="100%")
    btn(k, help_card, 968, 18, 144, 34, "Empty trash", "danger", name="EmptyTrash")

    empty = k.frame(scr, "TrashEmptyState", 1132, 170, x=280, y=470, fill=None, stroke="border-strong", radius=2, dashes=[4, 4])
    k.icon(empty, "trash", 24, "text-3", 1.4, x=538, y=40)
    txt(k, empty, "No more deleted games", 0, 80, size=15, color="text-2", weight="SemiBold", align="center", w=1132, lh="120%")
    txt(k, empty, "Games you remove appear here for 30 days before they are purged.", 0, 104, size=11, color="text-3", align="center", w=1132, lh="100%")
    txt(k, scr, "Deleted games keep their launches and save snapshots until you empty the trash.", 280, 820, size=11, color="text-3", lh="100%")
    return scr


# ---------------------------------------------------------------- build
def main():
    k = kit()
    library(k)
    library(k, "02 Library — View menu", x=1520, view_open=True)
    library(k, "03 Library — Filters", x=3040, filters_open=True)
    detail(k, x=4560)
    covers(k, x=6080)
    sync_screen(k, x=7600)
    add_game(k, x=9120)
    settings(k, x=10640)
    trash(k, x=12160)
    k.run(OUT)
    for name, file in [
        ("01 Library", "01-library.png"),
        ("02 Library — View menu", "02-library-view.png"),
        ("03 Library — Filters", "03-library-filters.png"),
        ("04 Detail", "04-detail.png"),
        ("05 Cover art", "05-covers.png"),
        ("06 Sync", "06-sync.png"),
        ("07 Add game", "07-add-game.png"),
        ("08 Settings", "08-settings.png"),
        ("09 Trash", "09-trash.png"),
    ]:
        figkit.Kit.export(OUT, name, os.path.join(RENDERS, file), 1)
        print("rendered", os.path.join(RENDERS, file))


if __name__ == "__main__":
    main()
