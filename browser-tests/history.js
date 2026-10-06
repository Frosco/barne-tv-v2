// Verifies the watch history's browser side.
//
// Runs the real static/app.js on the real production origin, with the
// deployed app.js/style.css/iframe_api aborted and a synthetic page + stubbed
// YT.Player in their place (the rig from return-to-grid.js). The stub
// player's current time is whatever the script sets in __spy.currentTime.
//
// `fetch` is a stub that records every call. page.route never sees a request
// a stubbed fetch makes, so the spy is the only way to observe the report --
// and it guarantees no test play ever reaches the family's real history.
//
// Case A: no report below 10 s of play; exactly one, carrying the tapped
//         video's ID, once it reaches 10 s; still one after more ticks.
// Case B: backing out at 3 s sends nothing; the next video reports normally.
// Case C: a reported video doesn't stop the next one from being reported.
// Case D: a clip that ends before 10 s is reported at its end; a video
//         already reported is not reported again when it ends.
// Case E: the wall's corner button stays fixed in the corner while the feed
//         scrolls, and the player covers it during a video.
// Case F: the history page's seedless grid plays a tile and returns to the
//         same tiles, without ever requesting /videos.
async (page) => {
  const ORIGIN = 'https://refsnes-barnetv.no';
  const PATH = '/__harness__';

  // seeded: true builds the wall (templates/index.html), false the history
  // page (templates/history.html) with a few entries.
  async function setup({ seeded }) {
    await page.unrouteAll();
    await page.route('**/static/app.js', r => r.abort());
    await page.route('**/static/style.css', r => r.abort());
    await page.route('**/iframe_api*', r => r.abort());
    await page.goto(ORIGIN + PATH);

    await page.evaluate((seeded) => {
      document.body.innerHTML = '';

      if (seeded) {
        document.body.insertAdjacentHTML('beforeend',
          '<a class="corner-button" href="/history" aria-label="Sett før"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg></a>');
      } else {
        document.body.insertAdjacentHTML('beforeend',
          '<a class="corner-button" href="/" aria-label="Tilbake til alle videoer"><svg viewBox="0 0 24 24" aria-hidden="true"><rect x="4" y="4" width="7" height="7" rx="1.5"/><rect x="13" y="4" width="7" height="7" rx="1.5"/><rect x="4" y="13" width="7" height="7" rx="1.5"/><rect x="13" y="13" width="7" height="7" rx="1.5"/></svg></a>' +
          '<h1 class="history-heading">Sett før</h1>');
      }

      const grid = document.createElement('div');
      grid.className = 'grid';
      if (seeded) {
        grid.setAttribute('data-seed', '12345');
        grid.setAttribute('data-next-offset', '60');
      }
      let html = '';
      for (let i = 0; i < (seeded ? 60 : 12); i++) {
        html += '<div class="grid-cell" data-video-id="v' + i + '"><span class="title">Video ' + i + '</span></div>';
      }
      grid.innerHTML = html;
      document.body.appendChild(grid);

      if (seeded) {
        const sentinel = document.createElement('div');
        sentinel.id = 'scroll-sentinel';
        document.body.appendChild(sentinel);
      }

      const pc = document.createElement('div');
      pc.id = 'player-container';
      pc.className = 'player-container';
      pc.hidden = true;
      pc.innerHTML = '<div id="player"></div>';
      document.body.appendChild(pc);

      // Only give the cells a height (no thumbnails here); everything else
      // comes from the real style.css injected next.
      const st = document.createElement('style');
      st.textContent = '.grid-cell{height:200px}';
      document.head.appendChild(st);

      window.__spy = { created: 0, destroyed: 0, onStateChange: null, currentTime: 0 };
      window.YT = {
        PlayerState: { ENDED: 0, PLAYING: 1, PAUSED: 2 },
        Player: function (id, opts) {
          window.__spy.created++;
          window.__spy.onStateChange = opts.events.onStateChange;
          this.destroy = function () { window.__spy.destroyed++; };
          this.getPlayerState = function () { return 1; };
          this.getDuration = function () { return 300; };
          this.getCurrentTime = function () { return window.__spy.currentTime; };
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

      // Record every call. An empty body means app.js marks the feed
      // exhausted and stops fetching.
      window.__fetches = [];
      window.fetch = (url, opts) => {
        window.__fetches.push({
          url,
          method: (opts && opts.method) || 'GET',
          body: opts && opts.body ? String(opts.body) : ''
        });
        return Promise.resolve({ ok: true, text: () => Promise.resolve('') });
      };
    }, seeded);

    await page.addStyleTag({ path: 'static/style.css' });
    await page.addScriptTag({ path: 'static/app.js' });
    await page.evaluate(() => window.onYouTubeIframeAPIReady());
  }

  const reports = () => page.evaluate(() => window.__fetches
    .filter(f => f.url === '/history' && f.method === 'POST')
    .map(f => f.body));

  const setTime = (t) => page.evaluate((v) => { window.__spy.currentTime = v; }, t);

  const openVideo = (id) => page.evaluate(
    (vid) => document.querySelector('.grid-cell[data-video-id="' + vid + '"]').click(), id);

  const exitFullscreen = () => page.evaluate(() => document.exitFullscreen());

  const end = () => page.evaluate(
    () => window.__spy.onStateChange({ data: window.YT.PlayerState.ENDED }));

  const results = {};

  // --- Case A: the 10 s threshold ---
  await setup({ seeded: true });
  await setTime(9.5);
  await openVideo('v20');
  await page.waitForTimeout(2500);
  const belowA = await reports();
  await setTime(10);
  await page.waitForTimeout(1500);
  const atA = await reports();
  await page.waitForTimeout(2500);
  const laterA = await reports();
  results.A_threshold = {
    belowThreshold: belowA, atThreshold: atA, later: laterA,
    verdict: {
      noReportBelowThreshold: belowA.length === 0,
      oneReportAtThreshold: atA.length === 1,
      reportCarriesId: atA[0] === 'id=v20',
      stillOneAfterMoreTicks: laterA.length === 1
    }
  };

  // --- Case B: an early back-out isn't a watch ---
  await setup({ seeded: true });
  await setTime(3);
  await openVideo('v20');
  await page.waitForTimeout(1500);
  await exitFullscreen();
  await page.waitForTimeout(500);
  const afterBackOutB = await reports();
  await setTime(12);
  await openVideo('v7');
  await page.waitForTimeout(1500);
  const afterNextB = await reports();
  results.B_backOutEarly = {
    afterBackOut: afterBackOutB, afterNext: afterNextB,
    verdict: {
      backOutNotReported: afterBackOutB.length === 0,
      nextVideoReported: afterNextB.length === 1 && afterNextB[0] === 'id=v7'
    }
  };

  // --- Case C: the next video after a reported one ---
  await setup({ seeded: true });
  await setTime(12);
  await openVideo('v20');
  await page.waitForTimeout(1500);
  await exitFullscreen();
  await openVideo('v7');
  await page.waitForTimeout(1500);
  const afterC = await reports();
  results.C_afterReportedVideo = {
    reports: afterC,
    verdict: {
      bothReported: afterC.length === 2 && afterC[0] === 'id=v20' && afterC[1] === 'id=v7'
    }
  };

  // --- Case D: videos that end by themselves ---
  await setup({ seeded: true });
  await setTime(8);
  await openVideo('v20');
  await page.waitForTimeout(300);
  await end();
  await page.waitForTimeout(2500);
  const afterShortD = await reports();
  await setTime(12);
  await openVideo('v7');
  await page.waitForTimeout(1500);
  await end();
  await page.waitForTimeout(2500);
  const afterLongD = await reports();
  results.D_endsEarly = {
    afterShortClip: afterShortD, afterLongVideo: afterLongD,
    verdict: {
      shortClipReportedOnEnd: afterShortD.length === 1 && afterShortD[0] === 'id=v20',
      noDoubleReportOnEnd: afterLongD.filter(b => b === 'id=v7').length === 1
    }
  };

  // --- Case E: the corner button ---
  const measureCorner = () => page.evaluate(() => {
    const btn = document.querySelector('a.corner-button');
    const r = btn.getBoundingClientRect();
    const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return {
      position: getComputedStyle(btn).position,
      rect: { top: Math.round(r.top), left: Math.round(r.left), width: Math.round(r.width), height: Math.round(r.height) },
      scrollY: Math.round(window.scrollY),
      hitIsButton: btn.contains(hit),
      hitInPlayer: document.getElementById('player-container').contains(hit),
      hit: hit ? hit.tagName + (hit.id ? '#' + hit.id : '') : null
    };
  });

  await setup({ seeded: true });
  const topE = await measureCorner();
  await page.evaluate(() => window.scrollTo(0, 800));
  const scrolledE = await measureCorner();
  await openVideo('v20');
  await page.waitForTimeout(300);
  const playingE = await measureCorner();
  results.E_cornerButton = {
    top: topE, scrolled: scrolledE, playing: playingE,
    verdict: {
      fixedPosition: topE.position === 'fixed',
      reachableBeforePlay: topE.hitIsButton && scrolledE.hitIsButton,
      staysInCornerWhenScrolled: scrolledE.scrollY === 800 && scrolledE.rect.top === topE.rect.top,
      coveredDuringPlay: playingE.hitInPlayer
    }
  };

  // --- Case F: the history page's seedless grid ---
  const snapshot = () => page.evaluate(() => ({
    cells: document.querySelectorAll('.grid-cell').length,
    firstIds: Array.from(document.querySelectorAll('.grid-cell')).slice(0, 5).map(c => c.dataset.videoId),
    scrollY: Math.round(window.scrollY),
    playerHidden: document.getElementById('player-container').hidden,
    created: window.__spy.created,
    fetched: window.__fetches.map(f => f.method + ' ' + f.url)
  }));

  await setup({ seeded: false });
  await page.waitForTimeout(1000);
  await page.evaluate(() => window.scrollTo(0, 99999));
  await page.waitForTimeout(1000);
  const beforeF = await snapshot();
  await openVideo('v3');
  await page.waitForTimeout(300);
  await exitFullscreen();
  await page.waitForTimeout(500);
  const afterF = await snapshot();
  results.F_seedlessGrid = {
    before: beforeF, after: afterF,
    verdict: {
      playedTile: afterF.created === 1,
      returnedToSameTiles: afterF.cells === beforeF.cells &&
        JSON.stringify(afterF.firstIds) === JSON.stringify(beforeF.firstIds),
      neverFetchedVideos: !afterF.fetched.some(f => f.includes('/videos'))
    }
  };

  return JSON.stringify(results, null, 2);
}
