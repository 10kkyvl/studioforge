# Building Roblox interfaces

StudioForge ships this file into every project it creates. It is ordinary project
context: edit it if your game needs different rules, and the agent will follow
yours instead.

None of the mistakes below produce an error. The console stays clean, a playtest
passes, and the problem shows up when someone opens the game on a phone. Most
Roblox sessions are on a phone.

## Sizing and position

`UDim2` has a Scale half and an Offset half, and the argument order interleaves
them: `UDim2.new(xScale, xOffset, yScale, yOffset)`. Scale is a fraction of the
parent, Offset is pixels.

```lua
-- Half the parent's width, a third of its height. Correct at any screen size.
frame.Size = UDim2.fromScale(0.5, 0.33)

-- 200x50 pixels. Correct only on the screen it was measured on.
frame.Size = UDim2.new(0, 200, 0, 50)

-- Mixed: full width, a fixed 48px tall row. This is a reasonable use of Offset.
row.Size = UDim2.new(1, 0, 0, 48)
```

Use Scale for anything that should keep its proportions across screens. Keep
Offset for things that genuinely are a fixed number of pixels: a border, padding,
a stroke, a one-row-tall header.

The difference is not subtle. A frame 200px wide covers a tenth of a 1920×1080
monitor and just over half of a 390×844 phone — measured, not estimated. Sized in
Scale it covers the same fraction of both.

`UDim2.fromScale(x, y)` and `UDim2.fromOffset(x, y)` are clearer than
`UDim2.new` when you only need one half.

## AnchorPoint

`AnchorPoint` is a `Vector2` from `(0, 0)` (top-left) to `(1, 1)` (bottom-right)
that chooses which point of the element `Position` refers to. Without it,
centring means subtracting half the element's own size from the parent's middle
by hand, which drifts as soon as either changes.

```lua
-- Centred at any parent size.
frame.AnchorPoint = Vector2.new(0.5, 0.5)
frame.Position = UDim2.fromScale(0.5, 0.5)

-- Pinned to the bottom-right corner, 12px in from each edge.
button.AnchorPoint = Vector2.new(1, 1)
button.Position = UDim2.new(1, -12, 1, -12)
```

## Layout objects

Parent a layout object into a container and it positions the container's children
for you, reflowing when anything resizes. Setting `Position` on each child by hand
is the thing these exist to replace.

- **`UIListLayout`** — a row or column. Set `FillDirection`, `Padding` (a `UDim`),
  and `HorizontalAlignment`/`VerticalAlignment`.
- **`UIGridLayout`** — a grid of equal cells. Set `CellSize` (a `UDim2` — use
  Scale for the cells too) and `CellPadding`.
- **`UIPadding`** — inner spacing on the container itself. Its four properties are
  `UDim`s, so padding can scale too.
- **`UIFlexItem`** on a child of a `UIListLayout` lets that child absorb the
  leftover space instead of every child being sized by hand.

Set `SortOrder = Enum.SortOrder.LayoutOrder` explicitly on any list or grid
layout and give each child a `LayoutOrder`. The default is
`Enum.SortOrder.Name`, so without this your menu is ordered alphabetically by
instance name and renaming an element silently reorders it.

```lua
local list = Instance.new("UIListLayout")
list.FillDirection = Enum.FillDirection.Vertical
list.SortOrder = Enum.SortOrder.LayoutOrder
list.Padding = UDim.new(0, 8)
list.HorizontalAlignment = Enum.HorizontalAlignment.Center
list.Parent = container
```

`AutomaticSize` (`Enum.AutomaticSize.X`, `.Y`, `.XY`) makes a container grow to
fit its laid-out children — useful for a tooltip or a dialog whose height depends
on its text. Set it on the container, not on the layout object.

## Constraints

Constraints clamp what a layout would otherwise let stretch.

- **`UIAspectRatioConstraint`** — keeps a square button square. Set `AspectRatio`
  (width ÷ height) and, if it matters which axis wins, `DominantAxis`. Without
  one, an icon sized in Scale becomes a rectangle on a wide monitor.
- **`UITextSizeConstraint`** — `MinTextSize` and `MaxTextSize`. Always pair it
  with `TextScaled = true`: `TextScaled` on its own will shrink text to
  unreadable on a phone and blow it up on a monitor.
- **`UISizeConstraint`** — `MinSize`/`MaxSize` in pixels, for an element that
  should scale but never below or above a usable size.

```lua
label.TextScaled = true
local textSize = Instance.new("UITextSizeConstraint")
textSize.MinTextSize = 14
textSize.MaxTextSize = 32
textSize.Parent = label
```

## Which surface to use

- **`ScreenGui`** — flat UI drawn over the camera. Parent it to
  `Players.LocalPlayer.PlayerGui` (or put it in `StarterGui` and let Roblox copy
  it). This is the default choice for menus, HUDs and shops.
- **`SurfaceGui`** — UI drawn onto a part's face, in the world. Set `Face`, and
  either parent it to the part or set `Adornee`. `PixelsPerStud` controls its
  resolution; `SizingMode` chooses whether it scales with the part.
- **`BillboardGui`** — UI that floats in the world and always faces the camera:
  nameplates, damage numbers, interaction prompts. `Size` is in studs-or-offset,
  `StudsOffset` moves it relative to its adornee, and `MaxDistance` stops it
  rendering from across the map.

`ScreenGui.ResetOnSpawn` defaults to `true`, so a `ScreenGui` you built at
runtime disappears when the player respawns. Set it to `false` for anything
persistent.

## Layering

Within one `ScreenGui`, `ZIndex` orders elements, and `ZIndexBehavior` decides
what that number is compared against. A `ScreenGui` from `Instance.new` starts as
`Enum.ZIndexBehavior.Global`, where every descendant is ranked by its raw `ZIndex`
across the whole tree regardless of nesting — so a child can punch out from behind
its own parent, and one element's `ZIndex = 10` can jump in front of an unrelated
panel somewhere else.

Set `ZIndexBehavior = Enum.ZIndexBehavior.Sibling` on the `ScreenGui`. Under it an
element is ordered only against its own siblings and a child never draws beneath
its parent, which is what makes a panel and its contents move as one thing. If
something then refuses to come to the front, it is because the container holding
it is behind — raise the container, not the child.

Between separate `ScreenGui`s, `DisplayOrder` decides: higher draws on top. Give
a modal or a notification layer its own `ScreenGui` with a high `DisplayOrder`
rather than fighting `ZIndex` inside a crowded one.

## The topbar and the safe area

`ScreenGui.IgnoreGuiInset` defaults to `false`, which insets the GUI below
Roblox's own topbar. That default is usually what you want: UI pinned to the top
starts below the Roblox button rather than under it.

Set `IgnoreGuiInset = true` only for something that should bleed to the physical
edge of the screen — a background image, a full-screen fade. When you do, keep
interactive elements out of the inset yourself. `GuiService:GetGuiInset()`
returns the offset as two `Vector2`s.

On phones, also leave room at the bottom for the jump and movement controls, and
remember that a notch or rounded corner can clip a corner-pinned element.
Anything the player must be able to press belongs away from the very edges.

## Input

Use `GuiButton.Activated` rather than `MouseButton1Click`: it fires for mouse,
touch and gamepad alike, so one connection covers every device.

```lua
button.Activated:Connect(function()
    -- runs on click and on tap
end)
```

`UserInputService.TouchEnabled` tells you the device has a touchscreen, and
`GuiService.SelectedObject` drives gamepad focus. Do not build two parallel UIs
for mouse and touch when one layout with `Activated` covers both.

Give touch targets room. A control that is comfortable to click with a mouse is
often too small to tap; sizing in Scale with a `UISizeConstraint` floor is the
usual way to guarantee a minimum.

## Making it feel finished

A grey `Frame` with a black border and default text reads as unfinished even when
the behaviour is correct. The cheap wins, in rough order of effect:

- **`UICorner`** — rounded corners. One instance, one `CornerRadius`.
- **`UIStroke`** — an outline that scales properly, with `Thickness`,
  `Color` and `ApplyStrokeMode`. Prefer it to `BorderSizePixel`, and set
  `BorderSizePixel = 0` on the frames underneath it.
- **`UIGradient`** — a gradient fill via `Color` (a `ColorSequence`) and
  `Rotation`.
- **`UIPadding`** — text that touches its own border is the single most common
  reason UI looks unfinished.
- **`CanvasGroup`** — fades or animates a whole group's transparency at once
  instead of every descendant separately.

Pick a small palette and reuse it. Two or three colours plus one accent, used
consistently, reads as designed; a different colour per element reads as
unfinished regardless of how much effort went in.

## Animation

`TweenService` animates any property, and a short tween on hover, press and open
is most of what makes UI feel responsive.

```lua
local TweenService = game:GetService("TweenService")
local info = TweenInfo.new(0.18, Enum.EasingStyle.Quad, Enum.EasingDirection.Out)
TweenService:Create(button, info, { Size = UDim2.fromScale(0.32, 0.11) }):Play()
```

Keep interface tweens short — roughly 0.1 to 0.25 seconds. Anything slower reads
as lag rather than polish. Animate `Size`, `Position`, `BackgroundTransparency`
and `UIScale.Scale`; a `UIScale` on a container is the simplest way to pop a
whole dialog in without touching each child.

## Before calling it done

Check the layout at more than one shape. In Studio, the emulator's device list
covers the common phone and tablet ratios, and dragging the viewport narrow is
enough to catch most of it. A tall phone and a wide monitor disagree about almost
every layout mistake, which is exactly why one screen is not evidence.

Specifically, look for: text that has collapsed or overflowed, buttons that have
become rectangles, elements overlapping the topbar or the jump button, and
anything that has drifted away from the edge it was supposed to be pinned to.
