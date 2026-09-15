(function () {
    "use strict";

    var ytReady = false;
    var player = null;
    var timeLeft = null;
    var timeLeftTimer = null;
    var playerContainer = document.getElementById("player-container");
    var grid = document.querySelector(".grid");
    var sentinel = document.getElementById("scroll-sentinel");

    var seed = grid ? grid.getAttribute("data-seed") : "";
    var nextOffset = grid ? parseInt(grid.getAttribute("data-next-offset"), 10) || 0 : 0;

    var PAGE_SIZE = 30;
    var MAX_TILES = 240;   // sliding-window cap on rendered cells
    var ROOT_MARGIN = 600; // px before the sentinel enters view to start loading
    // A video that runs out rests on black before the wall comes back, so the
    // end of one video doesn't slam straight into the next choice. A Back press
    // waits for nothing: the feed is already rendered underneath.
    var END_PAUSE_MS = 1500;

    var loading = false;
    var exhausted = false;
    var wasBackgrounded = false; // see the fullscreenchange handler at the foot

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

        // The player is a fixed, opaque overlay, so it covers the feed without
        // taking it out of layout. That is deliberate: the grid keeps its
        // height and scroll position while a video plays, so leaving the video
        // lands right back where the tap happened. Don't hide the grid here --
        // pruneTop() measures rows to compensate the scroll, and it needs them
        // laid out.
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

        // A dim countdown so a parent glancing over can see how long is
        // left before the natural break. It sits above the shield, so CSS
        // keeps pointer-events off it and a tap there still reaches the
        // shield below.
        timeLeft = document.createElement("div");
        timeLeft.id = "time-left";
        playerContainer.appendChild(timeLeft);
        updateTimeLeft();
        timeLeftTimer = setInterval(updateTimeLeft, 1000);

        playerContainer.requestFullscreen().catch(function () {
            // Fullscreen may be blocked by browser; video still plays
        });
    });

    // getDuration reads 0 until metadata arrives, and stays 0 for a live
    // stream. Neither has a remaining time to show, so the corner stays
    // empty rather than counting down from a wrong number.
    function updateTimeLeft() {
        if (!player || typeof player.getDuration !== "function") return;

        var duration = player.getDuration();
        if (!duration) {
            timeLeft.textContent = "";
            return;
        }
        var remaining = Math.max(0, Math.ceil(duration - player.getCurrentTime()));
        timeLeft.textContent = formatDuration(remaining);
    }

    // m:ss, growing to h:mm:ss past the hour. Seconds are rounded up by the
    // caller so the readout never sits at 0:00 while the video runs on.
    function formatDuration(seconds) {
        function pad(n) {
            return n < 10 ? "0" + n : String(n);
        }

        var hours = Math.floor(seconds / 3600);
        var minutes = Math.floor(seconds / 60) % 60;
        var rest = seconds % 60;

        if (hours > 0) return hours + ":" + pad(minutes) + ":" + pad(rest);
        return minutes + ":" + pad(rest);
    }

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
            returnToGrid(END_PAUSE_MS);
        }
    }

    // Tear down the player and uncover the feed, unchanged. Both natural end
    // and user exit (Escape / leaving fullscreen) lead here; pauseMs is how
    // long to sit on black first.
    function returnToGrid(pauseMs) {
        if (!player) return;

        clearInterval(timeLeftTimer);
        timeLeftTimer = null;
        timeLeft = null;

        // Destroy immediately to hide YouTube's end-screen recommendations.
        player.destroy();
        player = null;

        // This watch is over, so the next one starts from a clean slate: a
        // backgrounding during this video must not swallow the Back press
        // that ends the next one.
        wasBackgrounded = false;

        var div = document.createElement("div");
        div.id = "player";
        playerContainer.innerHTML = "";
        playerContainer.appendChild(div);

        // Uncover the feed that was there all along. Reloading would reshuffle
        // it, which punishes a mistaken tap: going back has to show the same
        // videos it did a moment ago.
        setTimeout(function () {
            if (document.fullscreenElement) {
                document.exitFullscreen();
            }
            playerContainer.hidden = true;
        }, pauseMs);
    }

    // Switching apps on Android drops element-fullscreen, but Chrome doesn't
    // deliver the fullscreenchange until the page returns to the foreground --
    // by then document.hidden is already false, so visibility at event time
    // can't tell a backgrounding-induced exit from a real one. Instead track
    // whether we were backgrounded while a video was playing; the first
    // fullscreen exit after that is the browser, not the user, so keep playing.
    // Only a fullscreen exit during uninterrupted foreground viewing is a
    // deliberate Back/Escape that should return to the grid.
    document.addEventListener("visibilitychange", function () {
        if (document.hidden && player) wasBackgrounded = true;
    });

    document.addEventListener("fullscreenchange", function () {
        if (document.fullscreenElement || !player) return;
        if (wasBackgrounded || document.hidden) {
            wasBackgrounded = false;
            return;
        }
        returnToGrid(0);
    });
})();
