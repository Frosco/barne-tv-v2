// Verifies that leaving a video returns to the SAME feed, in place.
//
// Runs the real static/app.js on the real production origin (so the page's
// own origin/CSP apply), with the deployed app.js/style.css/iframe_api
// aborted and a synthetic grid + stubbed YT.Player in their place.
//
// Case A: backing out of a video (fullscreen exit) keeps the same tiles and
//         scroll position, never navigates, and returns IMMEDIATELY.
// Case B: same when the video ends by itself, but resting on black first.
// Case C: after a video that was backgrounded mid-playback, the NEXT video
//         still honours a genuine Back press (wasBackgrounded must reset).
async (page) => {
  const ORIGIN = 'https://refsnes-barnetv.no';
  const PATH = '/__harness__';
  const END_PAUSE_MS = 1500;

  async function setup() {
    await page.unrouteAll();
    await page.route('**/static/app.js', r => r.abort());
    await page.route('**/static/style.css', r => r.abort());
    await page.route('**/iframe_api*', r => r.abort());
    await page.goto(ORIGIN + PATH);

    await page.evaluate(() => {
      document.body.innerHTML = '';

      const grid = document.createElement('div');
      grid.className = 'grid';
      grid.setAttribute('data-seed', '12345');
      grid.setAttribute('data-next-offset', '60');
      let html = '';
      for (let i = 0; i < 60; i++) {
        html += '<div class="grid-cell" data-video-id="v' + i + '"><span class="title">Video ' + i + '</span></div>';
      }
      grid.innerHTML = html;
      document.body.appendChild(grid);

      const sentinel = document.createElement('div');
      sentinel.id = 'scroll-sentinel';
      document.body.appendChild(sentinel);

      const pc = document.createElement('div');
      pc.id = 'player-container';
      pc.className = 'player-container';
      pc.hidden = true;
      pc.innerHTML = '<div id="player"></div>';
      document.body.appendChild(pc);

      // Only give the cells a height (no thumbnails here); everything else
      // comes from the real style.css injected next, so [hidden] resolves
      // exactly as it does in production.
      const st = document.createElement('style');
      st.textContent = '.grid-cell{height:200px}';
      document.head.appendChild(st);

      window.__spy = { created: 0, destroyed: 0, onStateChange: null };
      window.YT = {
        PlayerState: { ENDED: 0, PLAYING: 1, PAUSED: 2 },
        Player: function (id, opts) {
          window.__spy.created++;
          window.__spy.onStateChange = opts.events.onStateChange;
          this.destroy = function () { window.__spy.destroyed++; };
          this.getPlayerState = function () { return 1; };
          this.getDuration = function () { return 300; };
          this.getCurrentTime = function () { return 12; };
          this.pauseVideo = function () {};
          this.playVideo = function () {};
        }
      };

      let fsEl = null;
      Object.defineProperty(document, 'fullscreenElement', {
        configurable: true, get: () => fsEl
      });
      Element.prototype.requestFullscreen = function () {
        fsEl = this;
        document.dispatchEvent(new Event('fullscreenchange'));
        return Promise.resolve();
      };
      document.exitFullscreen = function () {
        fsEl = null;
        document.dispatchEvent(new Event('fullscreenchange'));
        return Promise.resolve();
      };

      let hiddenVal = false;
      Object.defineProperty(document, 'hidden', {
        configurable: true, get: () => hiddenVal
      });
      window.__setHidden = (v) => {
        hiddenVal = v;
        document.dispatchEvent(new Event('visibilitychange'));
      };

      // Empty page => app.js marks the feed exhausted and stops fetching.
      window.fetch = () => Promise.resolve({ ok: true, text: () => Promise.resolve('') });

      // Time the black-pause in-page, so MCP round-trip latency doesn't
      // pollute the measurement.
      window.__timeUntilHidden = (trigger) => {
        window.__hiddenAfterMs = null;
        const pc2 = document.getElementById('player-container');
        const t0 = performance.now();
        new MutationObserver(() => {
          if (pc2.hidden && window.__hiddenAfterMs === null) {
            window.__hiddenAfterMs = performance.now() - t0;
          }
        }).observe(pc2, { attributes: true, attributeFilter: ['hidden'] });
        trigger();
      };
    });

    await page.addStyleTag({ path: 'static/style.css' });
    await page.addScriptTag({ path: 'static/app.js' });
    await page.evaluate(() => window.onYouTubeIframeAPIReady());
  }

  const snapshot = () => page.evaluate(() => {
    const g = document.querySelector('.grid');
    const pc = document.getElementById('player-container');
    return {
      path: location.pathname,
      cells: document.querySelectorAll('.grid-cell').length,
      firstIds: Array.from(document.querySelectorAll('.grid-cell')).slice(0, 5).map(c => c.dataset.videoId),
      scrollY: Math.round(window.scrollY),
      gridHidden: g ? g.hidden : null,
      gridDisplay: g ? getComputedStyle(g).display : null,
      playerHidden: pc ? pc.hidden : null,
      pageHeight: Math.round(document.documentElement.scrollHeight),
      created: window.__spy ? window.__spy.created : null,
      destroyed: window.__spy ? window.__spy.destroyed : null
    };
  });

  const openVideo = (id) => page.evaluate(
    (vid) => document.querySelector('.grid-cell[data-video-id="' + vid + '"]').click(), id);

  const hiddenAfter = () => page.evaluate(
    () => window.__hiddenAfterMs === null ? null : Math.round(window.__hiddenAfterMs));

  function verdict(before, after) {
    return {
      stayedOnPage: after.path === PATH,
      sameCellCount: after.cells === before.cells,
      sameTiles: JSON.stringify(after.firstIds) === JSON.stringify(before.firstIds),
      sameScroll: after.scrollY === before.scrollY,
      playerHidden: after.playerHidden === true,
      gridVisible: after.gridHidden === false
    };
  }

  const results = {};

  // --- Case A: back out of a video, expect an immediate return ---
  await setup();
  await page.evaluate(() => window.scrollTo(0, 800));
  await page.waitForTimeout(100);
  const beforeA = await snapshot();
  await openVideo('v20');
  await page.waitForTimeout(200);
  const duringA = await snapshot();
  await page.evaluate(() => window.__timeUntilHidden(() => document.exitFullscreen()));
  await page.waitForTimeout(2200);
  const afterA = await snapshot();
  const msA = await hiddenAfter();
  results.A_backOut = {
    before: beforeA, during: duringA, after: afterA, hiddenAfterMs: msA,
    verdict: Object.assign(verdict(beforeA, afterA), { returnedPromptly: msA !== null && msA < 300 })
  };

  // --- Case B: video ends on its own, expect the black pause ---
  await setup();
  await page.evaluate(() => window.scrollTo(0, 800));
  await page.waitForTimeout(100);
  const beforeB = await snapshot();
  await openVideo('v20');
  await page.waitForTimeout(200);
  await page.evaluate(() => window.__timeUntilHidden(
    () => window.__spy.onStateChange({ data: window.YT.PlayerState.ENDED })));
  await page.waitForTimeout(2500);
  const afterB = await snapshot();
  const msB = await hiddenAfter();
  results.B_ended = {
    before: beforeB, after: afterB, hiddenAfterMs: msB,
    verdict: Object.assign(verdict(beforeB, afterB), {
      restedOnBlack: msB !== null && msB >= END_PAUSE_MS - 100 && msB < END_PAUSE_MS + 600
    })
  };

  // --- Case C: backgrounded video, then Back on the NEXT video ---
  await setup();
  await page.waitForTimeout(100);
  await openVideo('v3');
  await page.waitForTimeout(200);
  await page.evaluate(() => { window.__setHidden(true); window.__setHidden(false); });
  await page.evaluate(() => window.__spy.onStateChange({ data: window.YT.PlayerState.ENDED }));
  await page.waitForTimeout(2500);
  const midC = await snapshot();
  let afterC = midC;
  if (midC.path === PATH) {
    await openVideo('v7');
    await page.waitForTimeout(200);
    await page.evaluate(() => document.exitFullscreen());
    await page.waitForTimeout(2200);
    afterC = await snapshot();
  }
  results.C_backgroundedThenNext = {
    mid: midC,
    after: afterC,
    verdict: {
      firstReturnStayedOnPage: midC.path === PATH,
      secondVideoOpened: afterC.created === 2,
      backPressHonoured: afterC.destroyed === 2 && afterC.playerHidden === true
    }
  };

  return JSON.stringify(results, null, 2);
}
