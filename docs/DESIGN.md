# Design System

The client belongs to the lab's surface: one flat ink ground, the lab's two typefaces and the four accents. It is dense and quiet, and it is read-only. Nothing on the surface invites an action. Surfaces take a 6px corner, and nothing casts a shadow. The reference mock is `docs/web-client-mock.html`.

## Colours

| Token         | Hex       | Used for                          |
| ------------- | --------- | --------------------------------- |
| `ground`      | `#0c0d1d` | the page's one flat fill          |
| `panel`       | `#141522` | a grouped surface                 |
| `panelSunken` | `#0f1019` | a sunken fill                     |
| `textStrong`  | `#e6edf3` | the statement, names and headings |
| `text`        | `#c9d1d9` | prose and log entries             |
| `secondary`   | `#9aa0ae` | ledes and summaries               |
| `tertiary`    | `#8b93a1` | roles, dates and counts           |
| `line`        | `#20222e` | the hairline between rows         |
| `lineStrong`  | `#2b2e3d` | the hairline between groups       |
| `accent`      | `#7aede2` | a status mark                     |
| `agentGold`   | `#fdd684` | a status mark                     |
| `agentPink`   | `#f7a8d8` | a status mark                     |
| `agentMint`   | `#9fe6a6` | a status mark                     |
| `selection`   | `#232842` | the selected row's fill           |
| `alert`       | `#1d1a12` | an alert's fill                   |
| `placeholder` | `#79808c` | a muted mark                      |
| `okText`      | `#5e8874` | a muted mark                      |

`placeholder` and `okText` each clear 4.5:1 on both fills. No border, shadow or gradient separates a surface. Selection is a fill, never a bar or a heavier weight. Every status also carries a word, so it survives without colour.

## Typography

Space Grotesk sets names and the wordmark; Inter sets prose and labels; the system mono sets code and figures of reference. Figures use tabular numerals.

| Role      | Family        | Size   | Weight | Line | Tracking | Used for                     |
| --------- | ------------- | ------ | ------ | ---- | -------- | ---------------------------- |
| display   | Space Grotesk | 26px   | 600    | 1.2  | -0.02em  | a page's statement           |
| name      | Space Grotesk | 13.5px | 500    | 1.4  | -0.01em  | names and the wordmark       |
| body      | Inter         | 13.5px | 400    | 1.55 | —        | prose and log entries        |
| bodySmall | Inter         | 12.5px | 400    | 1.5  | —        | secondary prose              |
| label     | Inter         | 10px   | 500    | 1    | 0.08em   | a label above its value      |
| meta      | Inter         | 10.5px | 400    | 1.5  | —        | dates, counts and roles      |
| data      | ui-monospace  | 11px   | 400    | 1.65 | —        | code and a task's tool calls |

A task's tool calls are mono at 11px, one call per line, the name in ink and its arguments in the muted fill. The block shows twelve calls and then `+N more`; one call's arguments clip at 120 characters.

## Spacing

The base unit is 2px. The scale is 2, 4, 6, 8, 10, 12, 16, 20 and 26.

| Step   | Used for                                            |
| ------ | --------------------------------------------------- |
| `xxs`  | 2px, the focus ring's outline offset                |
| `xs`   | 4px, a step the scale reserves                      |
| `sm`   | 6px, a row's gap, and the 6px corner                |
| `md`   | 8px, an inline gap and the skip link's padding      |
| `lg`   | 10px, a label's margin and a row's padding          |
| `xl`   | 12px, the masthead's gap and a row's padding        |
| `xxl`  | 16px, a panel's padding and its top gap             |
| `section` | 20px, the gap above an alert                    |
| `gutter`  | 26px, the gutter between the reading column and the list |

Density comes from type and alignment, not from dividers. The surface keeps one reading column beside one list.
