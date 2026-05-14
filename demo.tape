# SafeCut CLI Demo — animated terminal recording.
#
# To generate an animated GIF + SVG from this file:
#   1. Install VHS:  go install github.com/charmbracelet/vhs@latest
#   2. Install deps: sudo apt install ttyd ffmpeg  (or brew install ttyd ffmpeg)
#   3. Run:          vhs demo.tape
#
# Requires: safecut in PATH, AZURE_SUBSCRIPTION_ID set.
# A static demo.svg is already committed for README use.

Output demo.gif

Set FontSize 14
Set Width 900
Set Height 700
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms
Set Shell bash

Type "safecut quick-scan"
Sleep 500ms
Enter
Sleep 22s
Sleep 3s
