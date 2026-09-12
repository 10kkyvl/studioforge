# Making a Roblox interface look built rather than generated

`roblox-ui.md` covers the mechanics — sizing, layout, constraints, layering. This
document covers what separates an interface that reads as a shipped game from one
that reads as a mockup. Everything below was verified against a live Studio, and
the places where the engine behaves unlike the documentation says are called out,
because those are where a confident guess quietly produces the wrong result.

## 1. Where the tree lives

Build the interface as real Instances under `StarterGui`, not as code that
constructs it at runtime. `StarterGui` is cloned into each player's `PlayerGui`
automatically, so a `LocalScript` that rebuilds the same tree on join earns
nothing and costs you a tree nobody can inspect, select or edit in Explorer.

The split practitioners actually use:

- **Static decoration is data.** Panels, frames, ribbons, badges, button faces,
  backgrounds — authored once, left as Instances.
- **Anything that changes at runtime is scripted.** Text that updates, bars that
  move, colours that swap with rarity, lists whose length depends on inventory.

The reason is concrete: text baked into an image cannot be translated or updated,
so it must stay a `TextLabel`; a panel border never changes, so it has no reason
to be rebuilt every join.

Repeated pieces belong in `ReplicatedStorage` as templates to clone. Do not leave
them in `Workspace` — they are prefabs, not scenery, and a model left in the world
sits in front of the player's camera.

## 2. Icons: three tiers, in order of preference

**Tier 1 — a real 3D model in a `ViewportFrame`.** This is what makes an icon look
like an icon rather than a shape. `generate_procedural_model` builds a model from
primitives; drop it into a `ViewportFrame` with its own `Camera` and it renders
inside the GUI with no image upload, no moderation wait, and no artist.

Frame it from the model's own bounds so any model lands centred:

```lua
local cf, ext = clone:GetBoundingBox()
local dist = ext.Magnitude * 0.5 * 2.4
local a, b = math.rad(yaw), math.rad(pitch)
local dir = Vector3.new(math.sin(a) * math.cos(b), math.sin(b), math.cos(a) * math.cos(b))
cam.CFrame = CFrame.new(cf.Position + dir * dist, cf.Position)
```

A distance of about 2.4x the bounding radius keeps the whole model in frame. Long
thin models such as a sword need a broadside yaw near 90 degrees or they are seen
end-on and vanish. Anchor every part of the clone and strip its scripts.

Static viewports are cheap — thirteen on one screen showed no cost. Resizing or
animating one forces a re-bake, so leave them still.

**Tier 2 — `Path2D` for outline glyphs.** Real vector paths, up to 100 control
points, `Closed` for a shape and open for a stroke. Good for chrome: a cross, a
chevron, a tick, a plus. It draws a contour only — there is no fill — so it cannot
stand in for an item icon.

**Tier 3 — `Frame` plus `UICorner`, `UIStroke` and `Rotation`.** Rounded plates,
rings, diamonds, bars. Legitimate and engine-supported, but limited to rectangles
and rounded rectangles; no organic silhouette comes out of it.

Only reach for an uploaded image when the icon needs painted texture that none of
the three can produce. Then: export at twice the display size, apply alpha
bleeding first — Roblox scales bilinearly and untreated transparent pixels leave a
dark fringe — and expect the decal upload to cap at 1024x1024 in practice even
though the docs say 4096.

## 3. Type

**`Font.new` does not fail on a family that does not exist.** It silently renders
the default face, and `pcall` reports success because creating the object is not
what fails. A font can only be confirmed by looking at rendered text. Several
plausible families — `TitanOne`, `Bungee`, `BowlbyOne`, `Chewy`, `LilitaOne`,
`SpaceMono`, `JetBrainsMono`, `IBMPlexMono`, `SourceCodePro` — are absent and fall
back to the default, which looks like the font being a bit thin rather than like
an error.

Confirmed present and heavy, in descending weight: `LuckiestGuy`, `Baloo2` at
Heavy, `Montserrat` at Heavy, `PassionOne` at Heavy, `BuilderSans` at Heavy.
Confirmed monospaced: `RobotoMono`, `Inconsolata`, and the legacy `Code`.

`Enum.Font` is deprecated for new work and, more usefully, is a strict subset of
what the font library holds — several families are reachable only through a
`rbxasset://fonts/families/<Name>.json` path.

Rules that hold up in a HUD:

- Roblox has **no letter-spacing property**. Tracked-out caps require one
  `TextLabel` per character or a baked image; the open feature request has no
  answer. Do not design a layout that depends on tracking.
- There is **no tabular-figure setting**. A readout that changes every second
  needs a monospaced family or its digits shuffle the line; everything else can
  take the heavier display face.
- Floor the scale. Below roughly 17px a HUD caption is unreadable at arm's length
  on a 1080p screen even though it looks fine zoomed in on a mockup. Resolve every
  size through one function so the floor cannot be forgotten at a call site.
- Raising the scale moves the layout. Change type size and re-check the screen in
  the same pass, or rows silently collide.

## 4. Depth without images

`UIShadow` is native as of June 2026: `BlurRadius` and `Spread` are `UDim` and
`UDim2`, not numbers, and it is faster than the 9-sliced image it replaces. It
does not affect layout and it stacks. Use it instead of a darker duplicate plate
offset behind the panel.

Text is different. Game text wants a heavy dark rim, not a drop shadow — an offset
copy under the glyphs reads as a blur at HUD sizes. **Two `UIStroke` children on
one object do not stack**, they merge, so a two-tone outline needs a duplicated
label underneath or a baked image. Scale the rim to the type size rather than
picking a thickness per call site.

Do not tween `UIStroke.Thickness` on text: it re-rasterizes every glyph each frame
and flickers. `UIStroke` also does not support Scale sizing and does not apply to
`ImageLabel` at all.

## 5. Masking to a rounded shape

`ClipsDescendants` clips to the raw rectangle and **ignores `UICorner`**. Anything
that must be bounded by a plate's rounded shape cannot be clipped into it.

The working technique is to let the plate paint the effect itself. A sheen sweep is
a layer the exact size of the button, with the button's own corner radius, filled
white, whose visibility is a `UIGradient` holding a narrow transparent-to-visible
band; animate the gradient's `Offset`. Because the highlight is painted by a shape
that already has the right silhouette, it is masked perfectly.

## 6. Motion

Drive scale through a `UIScale`, never through `Size`. `UIScale` carries the
stroke and corner radius with it; tweening `Size` leaves them behind and visibly
distorts the plate. Keep an animated `UIScale` off containers that also use
`AutomaticSize` with a list or grid layout — that combination has open engine bugs
and forces a relayout every frame.

**Cancel the running tween before starting its opposite.** Fast hover in and out
otherwise leaves the plate stuck part-way; this is the most reported UI bug on the
DevForum and it has no other fix.

Hang press feedback on `Activated` and `MouseButton1Down`/`Up`. `MouseEnter` and
`MouseLeave` are unreliable on touch, and Roblox is mostly touch — treat hover as a
bonus for desktop, never as the mechanism.

A set that reads as deliberate:

| Trigger | Property | From, to | Tween |
| --- | --- | --- | --- |
| hover | `UIScale.Scale` | 1, 1.06 | Back Out 0.15s |
| hover | `Rotation` | 0, 4 deg | Back Out 0.15s |
| press | `UIScale.Scale` | 0.94 | Sine Out 0.08s |
| release | `UIScale.Scale` | 1 | Back Out 0.20s |
| panel open | `UIScale.Scale` | 0, 1 | Back Out 0.30s |
| rare pulse | `UIStroke.Thickness` | 2.5, 6 | Sine InOut 0.9s, repeat -1, reverses |
| rare glow | `UIGradient.Rotation` | +360 | Linear 4s, repeat -1 |
| sheen | `UIGradient.Offset` | -1.1, 1.1 | Linear 1.4s, repeat -1, delay 0.9s |

`repeatCount = -1` with `reverses = true` is the self-sustaining loop; it needs no
per-frame connection to leak.

## 7. Before calling it done

Look at the screenshot as a player would, not as the author:

- Does every glow, shadow and gradient encode a state? If the element looks the
  same with the effect removed, remove it.
- Is any caption below the readability floor at full size, rather than zoomed in?
- Is there a deliberately quiet region next to a dense one? Uniform density is the
  same failure as uniform emptiness.
- Are the numbers believable — 8,420 out of 13,500, not 100 out of 100 — and are
  the labels real names rather than a placeholder?
- Does at least one element carry rendered art rather than a flat primitive?
- Do the rows still fit with the longest string and the fullest stack, not just
  with the tidy sample data?
