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

    // Video end detection
    function onPlayerStateChange(event) {
        if (event.data === YT.PlayerState.ENDED) {
            returnToGrid();
        }
    }

    // Clean up player and return to the grid
    function returnToGrid() {
        if (!player) return;

        playerContainer.classList.add("fade-out");

        setTimeout(function () {
            if (document.fullscreenElement) {
                document.exitFullscreen();
            }

            player.destroy();
            player = null;

            // Recreate the player div for next use
            var div = document.createElement("div");
            div.id = "player";
            playerContainer.innerHTML = "";
            playerContainer.appendChild(div);

            playerContainer.hidden = true;
            playerContainer.classList.remove("fade-out");
            grid.hidden = false;
            shuffleBtn.hidden = false;
        }, 2000);
    }

    // Handle fullscreen exit (e.g. user pressed Escape)
    document.addEventListener("fullscreenchange", function () {
        if (!document.fullscreenElement && player) {
            returnToGrid();
        }
    });

    // Shuffle button
    document.getElementById("shuffle-btn").addEventListener("click", function () {
        window.location = "/?shuffle=1";
    });
})();
