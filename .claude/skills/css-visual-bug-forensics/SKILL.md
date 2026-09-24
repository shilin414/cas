---
name: css-visual-bug-forensics
description: Diagnose a CSS/rendering visual bug (stray borders, stray outlines, wrong radii, misplaced boxes, focus rings, clipping) by measuring the actual rendered pixels instead of guessing at stylesheets. Use when a user reports a visual defect in a browser/WebView UI and the CSS "looks correct" — when the reported symptom does not match what the stylesheet text says should happen, or when a previous fix did not take effect.
---

# CSS visual bug forensics

A visual bug is a disagreement between **what the CSS says** and **what the
browser paints**. Reading stylesheets resolves the first; only the second tells
you which side is wrong. When the CSS looks correct and the symptom persists,
stop reading CSS — start measuring pixels.

## When to use this

- A user reports a stray border / outline / box / gap that the stylesheet
  "obviously" does not draw.
- A fix was applied and deployed, the deployed asset verifiably contains it,
  and the symptom is still there.
- You are about to guess a third CSS property to add.

## Procedure

### 1. Pin the symptom to geometry before touching code

Measure the defect in the **user's own screenshot**, not a mental model of it.
Convert pixels to CSS pixels using a known element for scale.

```python
from PIL import Image
im = Image.open(shot).convert("RGB"); px = im.load(); w, h = im.size
scale = VIEWPORT_CSS_WIDTH / w          # e.g. 390 / 354

def is_bug_color(c):                    # tune to the reported colour
    r, g, b = c
    return b > 120 and b - r > 40 and b - g > 25

# Locate each stroke, then convert to CSS px.
rows = [y for y in range(h) if sum(is_bug_color(px[x, y]) for x in range(0, w, 2)) > w * .05]
```

Then ask the decisive question: **which element is exactly this size?**

```python
print("box width css px:", 335 * scale)   # compare against candidate elements
```

In the mobile-composer case the measured box was 369 × 97 CSS px — matching
`.mobile-composer` to the pixel (390 − 2×12 gutter = 366, textarea 48 +
toolbar 48 = 96). That single arithmetic step eliminated "it is a wrapper
element" and "it is a stray overlay" and left only "it is this element's own
decoration".

**Do not skip the arithmetic.** "It looks bigger than the input" is what sent
earlier attempts after the wrong element; the numbers said it was exact.

### 2. Check whether the corner is a real arc or a hard corner

A hard corner on an element whose CSS says `border-radius` is the signature of
**browser-native chrome** (focus ring, appearance), not of a CSS box: CSS
shadows and borders follow `border-radius`; native rings do not.

```python
for y in range(top, top + 20):
    xs = [x for x in range(w) if is_bug_color(px[x, y])]
    if xs:
        print(y, xs[0], xs[-1], len(xs))
```

Flat `leftmost`/`rightmost` across rows (with a full-width row at the top)
means a rectangle. An arc means `leftmost` walks inward as `y` descends.

### 3. Reproduce minimally, then bisect the CSS

Build the smallest page that shows the bug, then add one candidate declaration
at a time. Serve it with Playwright (`channel="chromium"` — the headless shell
is often not installed):

```python
browser = p.chromium.launch(channel="chromium")
page = browser.new_context(viewport={"width": 390, "height": 844},
                           is_mobile=True, has_touch=True).new_page()
page.set_content(html)
page.click("textarea")
page.screenshot(path=f"case-{label}.png")
print(page.evaluate("() => getComputedStyle(document.querySelector('textarea')).outline"))
```

Each case is one screenshot you actually look at. Stop when one reproduces.

### 4. Trust the screenshot over the computed style

**The computed style can say `none` while the browser still paints.** A
computed `outline: none` alongside a visible ring is the tell that the paint
does not come from `outline` at all.

This is the trap that cost this investigation two wrong fixes:

| CSS | Painted result |
| --- | --- |
| `outline: none` (base rule) | ring present |
| `:focus { outline: none }` | ring present |
| `appearance: none` | ring gone |

`-webkit-appearance: auto` makes WebKit/Chromium paint a native focus ring that
hugs the element's rect and **ignores `border-radius`**. `outline: none` cannot
suppress it, because it is not an outline. Only resetting `appearance` does.

So: when a computed style disagrees with the pixels, believe the pixels and
look for a non-CSS paint source (native appearance, UA shadow DOM, an
`::before`/`::after`, a sibling overlay, `input`/`textarea`/`select` chrome).

### 5. Verify against the REAL stylesheets, not the minimal repro

A minimal repro isolates the cause; it does not prove the fix in context.
Render the component's actual markup against the project's real CSS files and
re-screenshot. Only then is the fix confirmed.

### 6. Confirm the fix is actually deployed before re-diagnosing

When a symptom survives a fix, fetch the deployed asset and grep it before
touching code again:

```bash
curl -s "$SITE/assets/index-<hash>.css" | grep -o "mobile-composer__textarea{[^}]*}"
```

If the fix is present in the deployed CSS, the cause is different or
additional — go back to step 1 with the new symptom. If absent, it is a
build/cache problem (also confirm the pipeline checks out the branch you
pushed: `git branch:` in the Jenkinsfile fetches the latest commit).

## Anti-patterns

- **Guessing a third property** after two failed CSS fixes. Two failures mean
  the model of the cause is wrong — go measure.
- **Reasoning from the stylesheet alone.** "The CSS says `outline: none`" is
  not evidence about pixels.
- **Trusting `getComputedStyle` over a screenshot.** See step 4.
- **Fixing the minimal repro and declaring done.** Verify against real CSS.
- **Leaving probe scripts and screenshots in the repo.** Delete them; keep the
  distilled finding in a code comment at the fix site.
