# Forge animation decision

Selected: design 3, the quiet workshop. A hammer and tongs hang above the anvil; only the furnace flames and sparse, spreading smoke animate. The billet rests directly on the anvil.

Implemented in `../splash.go` using the existing terminal palette, frame timer, skip behavior, and small-terminal fallback. The scene occupies 48 × 14 character cells.

The swinging hammer experiments were rejected because rotating character art appeared dotted or jagged. Static tools preserve legibility while the furnace supplies motion. The browser prototype has been removed after implementation.
