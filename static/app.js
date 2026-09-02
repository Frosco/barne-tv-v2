(function () {
    "use strict";

    var ytReady = false;
    var player = null;
    var playerContainer = document.getElementById("player-container");
    var grid = document.querySelector(".grid");
    var sentinel = document.getElementById("scroll-sentinel");

    var seed = grid ? grid.getAttribute("data-seed") : "";
    var nextOffset = grid ? parseInt(grid.getAttribute("data-next-offset"), 10) || 0 : 0;

    var PAGE_SIZE = 30;
    var MAX_TILES = 240;   // sliding-window cap on rendered cells
    var ROOT_MARGIN = 600; // px before the sentinel enters view to start loading

    var loading = false;
    var exhausted = false;

    // YouTube IFrame API ready callback
    window.onYouTubeIframeAPIReady = function () {
        ytReady = true;
    };

    // ---- Infinite scroll ----

    function columnCount() {
        var cols = getComputedStyle(grid).gridTemplateColumns.split(" ").length;
        return cols > 0 ? cols : 1;
    }

    // Keep the rendered cell count bounded. Remove whole rows from the top and
    // compensate scrollTop by their measured height so the viewport doesn't jump.
    function pruneTop() {
        var cells = grid.querySelectorAll(".grid-cell");
        var excess = cells.length - MAX_TILES;
        if (excess <= 0) return;

        var cols = columnCount();
        var removeCount = Math.floor(excess / cols) * cols;
        if (removeCount <= 0) return;

        var topBefore = cells[0].getBoundingClientRect().top;
        var topAfter = cells[removeCount].getBoundingClientRect().top;
        var removedHeight = topAfter - topBefore;

        for (var i = 0; i < removeCount; i++) {
            grid.removeChild(cells[i]);
        }
        window.scrollBy(0, -removedHeight);
    }

    function loadMore() {
        if (loading || exhausted || !grid || !seed) return;
        loading = true;

        var url = "/videos?seed=" + encodeURIComponent(seed) +
                  "&offset=" + nextOffset + "&count=" + PAGE_SIZE;

        fetch(url)
            // fetch only rejects on network failure, so a 4xx/5xx would other-
            // wise flow on as content: its body appended to the grid and the
            // next page requested at once, hammering the server in a tight loop.
            .then(function (resp) {
                if (!resp.ok) throw new Error("videos request failed: " + resp.status);
                return resp.text();
            })
            .then(function (html) {
                if (html.trim() === "") {
                    exhausted = true; // only happens when the pool is empty
                    if (observer) observer.disconnect();
                    loading = false;
                    return;
                }
                grid.insertAdjacentHTML("beforeend", html);
                nextOffset += PAGE_SIZE;
                pruneTop();
                loading = false;
                // Keep filling if the sentinel is still within reach (short
                // screens, fast scrolls). IntersectionObserver alone won't
                // re-fire while the sentinel stays continuously visible.
                checkSentinel();
            })
            // Re-check after a delay: the observer won't re-fire while the
            // sentinel stays visible, so a transient fetch error must self-heal.
            .catch(function () { loading = false; setTimeout(checkSentinel, 3000); });
    }

    function checkSentinel() {
        if (loading || exhausted || !sentinel) return;
        if (sentinel.getBoundingClientRect().top < window.innerHeight + ROOT_MARGIN) {
            loadMore();
        }
    }

    var observer = null;
    if (grid && sentinel && seed) {
        observer = new IntersectionObserver(function (entries) {
            if (entries[0].isIntersecting) loadMore();
        }, { rootMargin: ROOT_MARGIN + "px" });
        observer.observe(sentinel);
        // Initial fill in case the first page doesn't reach the sentinel.
        checkSentinel();
    }

    // ---- Playback ----

    if (grid) grid.addEventListener("click", function (e) {
        var cell = e.target.closest(".grid-cell");
        if (!cell || !ytReady) return;

        var videoId = cell.getAttribute("data-video-id");
        if (!videoId || player) return;

        grid.hidden = true;
        playerContainer.hidden = false;

        player = new YT.Player("player", {
            videoId: videoId,
            playerVars: { autoplay: 1, rel: 0, controls: 0 },
            events: { onStateChange: onPlayerStateChange },
        });

        // Creator end-screen cards live inside the player iframe, out of reach
        // of any page CSS or script. A shield over the iframe swallows every
        // tap so a card can never be followed, and drives playback itself so a
        // tap anywhere still pauses and resumes.
        var shield = document.createElement("div");
        shield.id = "click-shield";
        shield.addEventListener("click", togglePlayback);
        playerContainer.appendChild(shield);

        playerContainer.requestFullscreen().catch(function () {
            // Fullscreen may be blocked by browser; video still plays
        });
    });

    // Tap and the space key share this. getPlayerState only exists once the
    // player is ready, and an early key press can land before then.
    function togglePlayback() {
        if (!player || typeof player.getPlayerState !== "function") return;
        if (player.getPlayerState() === YT.PlayerState.PLAYING) {
            player.pauseVideo();
        } else {
            player.playVideo();
        }
    }

    // A laptop has no shield to tap, so space drives playback there. Only
    // intercept while a video is up, leaving space to scroll the grid, and
    // ignore auto-repeat so holding it down doesn't strobe play/pause.
    document.addEventListener("keydown", function (e) {
        if (!player || e.code !== "Space" || e.repeat) return;
        e.preventDefault();
        togglePlayback();
    });

    function onPlayerStateChange(event) {
        if (event.data === YT.PlayerState.ENDED) {
            returnToGrid();
        }
    }

    // Tear down the player and return to a fresh feed at the top. Both natural
    // end and user exit (Escape / leaving fullscreen) lead here.
    function returnToGrid() {
        if (!player) return;

        // Destroy immediately to hide YouTube's end-screen recommendations.
        player.destroy();
        player = null;

        var div = document.createElement("div");
        div.id = "player";
        playerContainer.innerHTML = "";
        playerContainer.appendChild(div);

        // Brief pause on black, then reload for a brand-new shuffled feed.
        setTimeout(function () {
            if (document.fullscreenElement) {
                document.exitFullscreen();
            }
            window.location = "/";
        }, 1500);
    }

    // Switching apps on Android drops element-fullscreen, but Chrome doesn't
    // deliver the fullscreenchange until the page returns to the foreground --
    // by then document.hidden is already false, so visibility at event time
    // can't tell a backgrounding-induced exit from a real one. Instead track
    // whether we were backgrounded while a video was playing; the first
    // fullscreen exit after that is the browser, not the user, so keep playing.
    // Only a fullscreen exit during uninterrupted foreground viewing is a
    // deliberate Back/Escape that should return to the grid.
    var wasBackgrounded = false;
    document.addEventListener("visibilitychange", function () {
        if (document.hidden && player) wasBackgrounded = true;
    });

    document.addEventListener("fullscreenchange", function () {
        if (document.fullscreenElement || !player) return;
        if (wasBackgrounded || document.hidden) {
            wasBackgrounded = false;
            return;
        }
        returnToGrid();
    });
})();
