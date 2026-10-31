# ADR 0010: Dashboard themes

**Status:** Accepted

## Context

The dashboard adopts two style references. Supabase is dark and flat; Column is light with
soft card shadows. They cannot be merged into one theme.

## Decision

The dashboard follows the system color scheme until the user picks a theme with the switch
in the top bar, which is remembered in the browser. Dark mode takes its tokens from Supabase
and light mode from Column. Both live as CSS variables in `web/src/styles.css`.

Inter and JetBrains Mono stand in for the references' commercial fonts, Circular and
SuisseIntl. Both are bundled through Fontsource, so the dashboard makes no external requests.

## Departures from the references

- Baseline and candidate use blue and orange in both themes. Supabase allows only greens, and
  grey or navy against an accent failed the palette validator. Green is kept for the brand
  accent and the "no regression" state, so a series can never read as a verdict.
- Red and amber remain for regression states, with an icon and a label.
- Light muted text is Slate Ink (`#3b3e47`), not Steel (`#7c7f88`), which is 3.71:1 on the
  canvas and below the WCAG AA minimum of 4.5:1.
- Stat numbers stay in the text color, not Column's seafoam, so a slow p95 is not shown in green.
- Dark cards use `#171717` and the page top has a faint glow, as the live Supabase site does,
  although the reference lists neither. The texture is decoration in CSS and carries no data.
- Code uses no expanded tracking, which hurts long paths and JSON bodies.
- Table links use the text color in dark mode, keeping green to about one element per region.

## Consequences

The layouts borrow the references too: display headlines, card grids, mono section tags, and a
floating card for the verdict rules. Run headlines are computed from the report.

Generated `report.html` files keep their own styling.
