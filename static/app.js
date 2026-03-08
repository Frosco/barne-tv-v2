(function () {
    "use strict";

    var ytReady = false;
    var player = null;
    var playerContainer = document.getElementById("player-container");
    var grid = document.querySelector(".grid");
    var shuffleBtn = document.getElementById("shuffle-btn");

    // YouTube IFrame API ready callback
    window.onYouTubeIframeAPIReady = function () {
        ytReady = true;
    };

    // Thumbnail click handlers
    grid.addEventListener("click", function (e) {
        var cell = e.target.closest(".grid-cell");
        if (!cell || !ytReady) return;

        var videoId = cell.getAttribute("data-video-id");
        if (!videoId || player) return;

        grid.hidden = true;
        shuffleBtn.hidden = true;
        playerContainer.hidden = false;

        player = new YT.Player("player", {
            videoId: videoId,
            playerVars: { autoplay: 1, rel: 0, modestbranding: 1 },
            events: { onStateChange: onPlayerStateChange },
        });

        playerContainer.requestFullscreen().catch(function () {
            // Fullscreen may be blocked by browser; video still plays
        });
    });

    // Video end detection — load fresh selection
    function onPlayerStateChange(event) {
        if (event.data === YT.PlayerState.ENDED) {
            returnToGrid(true);
        }
    }

    // Clean up player and return to the grid
    // shuffle=true: video ended naturally, load new videos
    // shuffle=false: user pressed Escape, show same grid
    function returnToGrid(shuffle) {
        if (!player) return;

        // Destroy immediately to hide YouTube's end-screen recommendations
        player.destroy();
        player = null;

        // Recreate the player div for next use
        var div = document.createElement("div");
        div.id = "player";
        playerContainer.innerHTML = "";
        playerContainer.appendChild(div);

        // Brief pause on black screen, then back to grid
        setTimeout(function () {
            if (document.fullscreenElement) {
                document.exitFullscreen();
            }
            if (shuffle) {
                window.location = "/?shuffle=1";
            } else {
                playerContainer.hidden = true;
                grid.hidden = false;
                shuffleBtn.hidden = false;
            }
        }, 1500);
    }

    // Handle fullscreen exit (e.g. user pressed Escape) — keep same grid
    document.addEventListener("fullscreenchange", function () {
        if (!document.fullscreenElement && player) {
            returnToGrid(false);
        }
    });

    // Shuffle button
    document.getElementById("shuffle-btn").addEventListener("click", function () {
        window.location = "/?shuffle=1";
    });
})();
