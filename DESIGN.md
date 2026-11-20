---
version: alpha
name: Dispatch web client
description: A quiet, read-only surface in the lab's ink and type.
omitted:
  components: the mock is the reference; this file holds no component anatomy
colors:
  ground: "#0c0d1d"
  panel: "#141522"
  panelSunken: "#0f1019"
  textStrong: "#e6edf3"
  text: "#c9d1d9"
  secondary: "#9aa0ae"
  tertiary: "#8b93a1"
  line: "#20222e"
  lineStrong: "#2b2e3d"
  accent: "#7aede2"
  agentGold: "#fdd684"
  agentPink: "#f7a8d8"
  agentMint: "#9fe6a6"
  selection: "#232842"
  alert: "#1d1a12"
  placeholder: "#79808c"
  okText: "#5e8874"
typography:
  display:
    fontFamily: Space Grotesk
    fontSize: 26px
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: -0.02em
  name:
    fontFamily: Space Grotesk
    fontSize: 13.5px
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: -0.01em
  body:
    fontFamily: Inter
    fontSize: 13.5px
    fontWeight: 400
    lineHeight: 1.55
  bodySmall:
    fontFamily: Inter
    fontSize: 12.5px
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: Inter
    fontSize: 10px
    fontWeight: 500
    lineHeight: 1
    letterSpacing: 0.08em
  meta:
    fontFamily: Inter
    fontSize: 10.5px
    fontWeight: 400
    lineHeight: 1.5
  data:
    fontFamily: ui-monospace
    fontSize: 11px
    fontWeight: 400
    lineHeight: 1.65
rounded:
  sm: 6px
spacing:
  xxs: 2px
  xs: 4px
  sm: 6px
  md: 8px
  lg: 10px
  xl: 12px
  xxl: 16px
  section: 20px
  gutter: 26px
---

# Dispatch web client

## Overview

The client belongs to the lab's surface: one flat ink ground, the lab's two typefaces and the four accents. It is dense and quiet, and it is read-only. Nothing on the surface invites an action.

The operator opens it to explain one record, not to watch a dashboard. A scan comes before a read: names first, then detail. The surface does not animate; a change appears in place.

## Colors

The ground is `{colors.ground}`; a panel is `{colors.panel}` and a sunken fill `{colors.panelSunken}`. No border, shadow or gradient separates them.

Text has four tiers: `{colors.textStrong}`, `{colors.text}`, `{colors.secondary}`, `{colors.tertiary}`. `{colors.line}` is the hairline between rows. `{colors.placeholder}` and `{colors.okText}` are the two muted marks; each clears 4.5:1 on both fills.

Selection is the `{colors.selection}` fill. It is never a bar, a border or a heavier weight.

Status uses the four accents: `{colors.accent}`, `{colors.agentGold}`, `{colors.agentPink}` and `{colors.agentMint}`. Every status also carries a word, so it survives without colour.

## Typography

Space Grotesk sets names and the wordmark; Inter sets prose and labels; the system mono sets code and figures of reference. The families and weights are the `typography` tokens; a size outside the set is set on its element. Figures use tabular numerals.

A task's tool calls are mono at 11px, one call per line, the name in ink and its arguments in the muted fill. The block shows twelve calls and then `+N more`; one call's arguments clip at 120 characters.

## Layout

Spacing starts from the `spacing` scale; a surface's own padding is measured on it. One gutter separates groups; density comes from type and alignment, not from dividers. The surface keeps one reading column beside one list.

## Elevation & Depth

There is no elevation. The panel fill and one hairline do the separation. Nothing casts a shadow.

## Shapes

Surfaces take `{rounded.sm}`. There are no accent bars and no left borders.

## Do's and Don'ts

- Do keep the ink ground, the two families and the four accents.
- Do use the panel fill for a grouped surface. Do not draw a border or a shadow.
- Do mark selection with a fill. Do not use a left bar.
- Do keep list text at weight 400 or 500. Do not bold the selected row.
- Do keep the surface read-only. Do not add controls that imply a change.
- Do set the wordmark in Space Grotesk, alone. Do not add a mark, an illustration, a gradient or an accent wash.
- Do keep code in mono and prose in Inter. Do not set a JSON body in Space Grotesk.

The reference mock is `docs/web-client-mock.html`, checked at 1440 and 1024.
