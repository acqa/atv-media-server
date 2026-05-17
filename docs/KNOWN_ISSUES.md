# Known issues

Issues we hit during real-device testing on Apple TV 3 (model `AppleTV3,2`, firmware 7.9, build `12H1006`) that we chose to live with instead of fixing.

---

## 1. Seasons shelf misrenders with 7+ items on the show page

**Where**: [show.xml](../server/internal/appletv/templates/show.xml), the `<centerShelf>` of season `<actionButton>` tiles.

**Symptoms** observed on a 13-season show (Columbo):

- Items 1–6 render and navigate normally.
- When focus reaches Season 7, the row tries to scroll horizontally but draws ghost text: "Season 5 6", "Season 6" split across cells.
- At Season 8 the focused button disappears off-screen; only the word "Season" stays where the missing button should be, and the number "6" lingers near the left edge.
- By Season 9–11 the visible buttons stop updating entirely; focus is somewhere off-screen but the row stops repainting.
- Pressing Select still navigates to the correct season — only the visual state is broken.
- Closing and reopening the show repaints correctly around the remembered focus position (so the renderer itself can show items 8–13, it just can't animate to them).

**Root cause**: ATV3 firmware 7.9 mishandles horizontal-scroll focus animation in `<shelf>` once items exceed `columnCount`. The bug is in the firmware, not in our XML — both `<centerShelf>` and `<bottomShelf>` reproduce it, with both `<actionButton>` and `<moviePoster>` items, with or without `center="true"`.

**Affects**: only shows with 7+ seasons. In our current library that's just Columbo. Everything ≤6 seasons works without visible artefacts.

**Why we're not fixing it**: every alternative layout that does fix the bug looks worse:

| Alternative | Fixes overflow | Why we rejected it |
|---|---|---|
| `<bottomShelf>` + `<moviePoster>` | yes | Stretched-rectangle tiles with the label below them looked worse than the square actionButtons with the label inside. |
| `<bottomShelf>` + `<actionButton>` | no | Same focus-scroll bug, plus the tiles render taller than in `<centerShelf>`. |
| `<centerShelf center="true">` + many items | no | Original variant. The whole row anchors to the screen centre; with 13 items the first ~4 fall off the left edge and draw over the poster. |
| `<listWithPreview>` with vertical season menu | yes | Cleanest from a rendering POV (this is what PlexConnect's `TVShow/Season_List.xml` does), but the page loses the moviePoster + summary layout — sezones become a plain text list with a preview pane. Visually a step back. |
| Split into two pages: `/show.xml` (details) → `/seasons.xml` (picker) | yes | Two clicks to get to a season. Worse UX for the 90% of shows that have ≤6 seasons. |
| Dynamic `columnCount = len(seasons)` capped high | partially | Tiles become very narrow when there are many seasons. Focus animation still glitches at the right edge. |

**Workaround**: live with it. The bug is purely cosmetic and only on long shows; navigation works (Select on a "ghost" position lands on the right season).

**Where to look if we want to revisit**:

- ATV3 SDK documentation on `<shelf>` and `<centerShelf>` — we don't have a definitive reference, so the focus-scroll behaviour is poorly understood.
- PlexConnect uses `<itemDetail>` + `<bottomShelf>` of episodes (not seasons) in `TVShow/PrePlay.xml`, and `<listWithPreview>` for season selection in `TVShow/Season_List.xml`. Both work on ATV3 but make different UX trade-offs than we currently have.
- The `<listWithPreview>` rewrite is preserved in git history at the commit message "switch show.xml to listWithPreview for season selection" — easy to resurrect if priorities change.

---

## 2. TMDb (`api.themoviedb.org` / `image.tmdb.org`) is DNS-blocked at the user's ISP

**Where**: [server/internal/metadata/](../server/internal/metadata/) and [server/internal/server/posters.go](../server/internal/server/posters.go).

**Symptoms** in logs:

```
TMDb "Питер FM": dial tcp [::1]:443: connect: connection refused
warm episodes: Get "https://image.tmdb.org/t/p/w780/...jpg": dial tcp 127.0.0.1:443: connect: connection refused
```

DNS resolves TMDb hostnames to `[::1]` / `127.0.0.1` (loopback). Our Go process tries to connect to localhost:443, where the container's own HTTPS server is listening — TLS handshake succeeds but the server presents its `appletv.redbull.tv` certificate, which doesn't match `image.tmdb.org`, so the resolver-cache fetch fails with a `x509: certificate is valid for appletv.redbull.tv, not image.tmdb.org` error.

**Root cause**: ISP-level DNS sinkholing of TMDb (Russian RKN). Docker's resolver inherits from the host, which inherits from the upstream provider.

**Why we're not fixing it in code**: workarounds in code (DoH, hardcoded resolver to 1.1.1.1, etc.) would add complexity for a problem the user can solve operationally.

**Workaround**: run the server once on a network with TMDb access (or via a VPN on the host). The warm pass after the initial scan downloads every poster size the templates request and saves them under `data/posters/`. Subsequent runs without TMDb access serve everything from the on-disk cache without making any outbound TMDb calls.

This is the use case the `WarmMovies` / `WarmSeries` / `WarmEpisodes` helpers were designed for (see [server/internal/server/posters_warm.go](../server/internal/server/posters_warm.go)).

---

## 3. `<preview><link>` inside `<navigationItem>` crashes the home screen

**Where**: [main.xml](../server/internal/appletv/templates/main.xml).

**Symptoms**: ATV3 shows a generic "sample-xml is currently unavailable. Try again later." on launch, before requesting the preview URL.

**Root cause**: `<preview><link>` is a valid ATV3 element inside `<oneLineMenuItem>` (PlexConnect uses it in `Library/List.xml`), but it is NOT supported inside `<navigationItem>` on firmware 7.9. The page parser rejects the whole document instead of skipping the unknown child.

**Status**: fixed-by-removal. `main.xml` is back to plain navbar with no `<preview>` element. The endpoints `/preview-movies.xml` and `/preview-series.xml` and their templates remain in the codebase, ready to be re-wired the moment we adopt a `<listWithPreview>`-style home screen.

Regression test: [`TestMainHandler_NoPreviewLinksInNavbar`](../server/internal/server/preview_handlers_test.go) guards against re-adding `<preview>` inside the navbar.
