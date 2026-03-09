# Anchor Web UI

The web UI is a single-page app using vendored Preact + HTM with no build step and no external resources. All static files are embedded into the Go binary via `//go:embed` in `embed.go`.

## Files

- `index.html` — SPA entry point. Contains a synchronous theme-init script in `<head>` to prevent flash of wrong theme.
- `style.css` — Complete design system. All styling lives here; no inline styles in JS except rare one-off layout tweaks.
- `app.js` — All components, routing, state management, and API calls.
- `vendor/` — Vendored Preact, Preact Hooks, and HTM. Do not modify.

## Design System

### Color Palette

The palette uses **warm neutrals** (stone tones, not cold grays) with a **teal accent**. All colors are defined as CSS custom properties in `:root` (light) and `[data-theme="dark"]` (dark).

Key token groups:
- `--bg-*` — page, surface, hover, inset backgrounds
- `--text-*` — primary, secondary, tertiary text
- `--border` / `--border-strong` — subtle and emphasized borders
- `--accent` / `--accent-hover` / `--accent-subtle` — teal action color
- `--status-{ok,warn,error,info}` — each has base, `-bg`, `-text`, and `-border` variants
- `--terminal-*` — always-dark colors for module output blocks

Status color mapping:
- **Red** (`--status-error`): errors, unhealthy agents, destructive actions
- **Amber** (`--status-warn`): stale agents, connection issues
- **Green** (`--status-ok`): healthy agents, successful module runs
- **Blue** (`--status-info`): changed status, direct assignment badges, informational

### Dark Mode

Toggled via `data-theme` attribute on `<html>`. The theme-init script in `index.html` reads from `localStorage('anchor-theme')` or falls back to `prefers-color-scheme`. The toggle button in the header persists the choice.

When adding new colors, always define both light and dark variants. The dark palette uses brighter accent/status colors for readability on dark backgrounds.

### Typography

- Body: `"Avenir Next", "Avenir", "Segoe UI", system-ui, sans-serif` (`--font-body`)
- Mono: `"Berkeley Mono", "JetBrains Mono", "Fira Code", "Cascadia Code", ui-monospace, monospace` (`--font-mono`)
- Base size: 15px

Use mono for: filenames, UUIDs, code snippets, module output, terminal content.

### Component Patterns

**Cards/surfaces**: White background (`--bg-surface`), 1px `--border`, `var(--radius)` corners (6px), `var(--shadow)` box-shadow.

**Status borders**: 4px left border on agent cards and health summary cells, colored by health status. Use `border-left: 4px solid var(--status-*)` with `border-radius: 0 var(--radius) var(--radius) 0`.

**Section headings**: Small uppercase labels with letter-spacing, bottom border. Class: `.section-title`.

```css
font-size: 0.6875rem;
font-weight: 600;
text-transform: uppercase;
letter-spacing: 0.05em;
color: var(--text-tertiary);
```

**Badges**: Pill-shaped (`.badge`) with status-colored background, border, and text. Use `.badge--ok`, `.badge--changed`, `.badge--error`.

**Tag pills**: Rounded pills (`.tag-pill`) with inset background and optional `×` remove button (`.tag-pill-remove`). Use `.tag-pill--small` inside agent cards.

**Buttons**: Base class `.btn` with modifiers:
- `.btn--primary` — teal accent, for main actions
- `.btn--danger` — red, for destructive actions
- `.btn--secondary` — gray/inset, for cancel/secondary actions
- `.btn--icon` — minimal, for inline remove buttons (turns red on hover)
- `.btn--sm` — smaller padding for inline use

**Forms**: `.form-row` is a flex container with gap. Inputs and selects stretch (`flex: 1`), buttons don't (`flex-shrink: 0`). Focused inputs show a teal border with subtle shadow ring.

**Module output**: Always uses dark terminal colors (`--terminal-*`) regardless of theme. Pre-wrapped monospace text.

**Help content**: `.help-content` cards with `.help-dl` definition lists using a two-column grid layout.

### Layout

- Max width: 960px, centered with `margin: 0 auto`
- Container padding: `1.25rem 1rem`
- Header: sticky, white/dark surface, 3px teal top border (brand element)
- Navigation: pill-shaped links with active state highlighting in accent color

### Behavior Patterns

**Error handling**: Error banners (`.error-banner`) do NOT auto-dismiss. User must click to close. Error states always include a retry button and visible navigation.

**Loading**: Pulsing "Loading..." text. Navigation always renders during loading.

**Empty states**: Centered text with helpful guidance (`.empty-state`). Include actionable hints.

**Connection tracking**: Module-level tracker counts consecutive network failures. Green/amber/red dot in header. Stale data banner appears between header and content when connection degrades.

**Polling**: Dashboard and agent detail poll every 10 seconds. Other pages load once.

**Inline editing**: Agent names are click-to-edit. Enter to save, Escape to cancel. No separate edit button.

**Auto-expand**: Module results with error status auto-expand on first load. User can collapse them; subsequent polls don't re-expand.

**Confirm dialogs**: Only for destructive actions (deleting tags, removing module assignments). Tag removal from agents has no confirm since it's easy to re-add.

### Adding New Pages

1. Create a component function following existing patterns (back link, page title, sections).
2. Add a route case in `App()`.
3. Add a nav link in `Header()` — update the Dashboard `isActive` check to exclude new paths.
4. Use existing CSS classes; add new ones to `style.css` only if needed.
