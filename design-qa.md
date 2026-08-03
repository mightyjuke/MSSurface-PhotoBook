# Design QA

## Comparison target

- Source visual truth: the user's compact Home Assistant-style weather-card reference in this conversation (797 x 463 px attachment).
- Implementation evidence: the user's Surface RT viewport screenshot in this conversation (1455 x 816 px attachment, containing a 1366 x 768 display area).
- CSS viewport: 1366 x 768 at assumed device scale factor 1.
- State: Windows mock mode, clock enabled, mixed-aspect portrait photograph visible, browser location permitted.
- Revised local preview: `http://127.0.0.1:18080/display/`.

## Findings and iteration history

- [P1] Initial widget obscured too much of the photograph.
  - Evidence: the implementation screenshot shows the 560 x approximately 313 px card occupying about 41% of the display width and 41% of its height. The user confirmed it was too large on the Surface RT.
  - Fix: reduced the desktop card first to 420 px and then to 360 px after portrait-photo testing, reduced its inset from 24 px to 16 px, and proportionally tightened the icon and metrics columns while preserving clock and forecast legibility.
  - Expected revised footprint: about 26% of display width and 27% of display height, leaving separation between the portrait image boundary and the widget border.
  - Post-fix visual evidence: blocked because no controllable browser is available for a revised capture.

## Fidelity surfaces

- Fonts and typography: the same system font stack and hierarchy are retained; clock size reduced from 80 px to approximately 58 px and forecast labels from 13 px to approximately 11 px.
- Spacing and layout rhythm: card width reduced by 25%; estimated height reduced by about 32%; padding, gaps, radius, shadow, and edge insets were reduced proportionally.
- Colors and tokens: user feedback replaced the green-tinted surface with neutral black, subsequently lightened from 78% to 64% opacity. The border and empty forecast tracks are neutral white overlays; the white foreground, yellow current-condition icon, and temperature range colors remain unchanged.
- Image and icon quality: the photograph presentation is unchanged. Weather Icons remain vector SVG assets from the existing Iconify integration.
- Copy and content: current conditions, date, temperature, humidity, today's range with a circular live-temperature marker, apparent temperature, wind, and rain probability remain present.

## Evidence limits

- Full-view comparison: completed against the user's supplied pre-fix Surface screenshot; it established the oversized-card finding.
- Focused-region comparison: not completed because a revised implementation screenshot cannot be captured in this session.
- Functional verification: Go tests, vet, JavaScript parsing, icon endpoints, and the shared browser-location weather path were verified.

## Color revision

- User finding: the green card background did not suit the slideshow.
- Fix: changed the card to `rgba(8, 9, 12, .64)` and removed the remaining green tint from its border and empty temperature tracks.
- Performance constraint: no live `backdrop-filter` was added, avoiding extra compositing work on the Surface RT.

final result: blocked

Blocker: no controllable browser is available to capture and compare the revised 1366 x 768 implementation. The next visual iteration should use the user's screenshot of the revised mock.
