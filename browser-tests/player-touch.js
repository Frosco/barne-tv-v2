// Guards the two touch behaviours of the player overlay, in a touch-capable
// phone-sized context:
//
//  1. `touch-action: none` on .player-container stops a drag on the video from
//     scrolling the feed underneath. A fixed element is not a scroll
//     container, so without that rule the gesture chains to the document and
//     leaving the video lands somewhere else. The player is not always
//     fullscreen -- an Android app-switch drops it, and requestFullscreen can
//     be refused -- so this is reachable in normal use. A control drag on the
//     bare grid proves the gesture works in the rig before trusting the
//     blocked one.
//  2. That same rule must NOT break tap-to-pause on the click-shield.
//
// Note: tap immediately after a synthetic drag and the first tap gets eaten by
// the input pipeline, which looks exactly like a broken shield. The taps get a
// clean settle here on purpose.
async (page) => {
  const browser = page.context().browser();
  const ctx = await browser.newContext({
    hasTouch: true, isMobile: true, viewport: { width: 412, height: 915 }
  });
  const p = await ctx.newPage();
  const cdp = await ctx.newCDPSession(p);

  const drag = async (fromY, toY) => {
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: 200, y: fromY }] });
    for (let y = fromY; y >= toY; y -= 40) {
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: 200, y }] });
    }
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    await p.waitForTimeout(800);
  };

  try {
    await p.route('**/static/app.js', r => r.abort());
    await p.route('**/static/style.css', r => r.abort());
    await p.route('**/iframe_api*', r => r.abort());
    await p.goto('https://refsnes-barnetv.no/__harness__');

    await p.evaluate(() => {
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

      const st = document.createElement('style');
      st.textContent = '.grid-cell{height:200px}';
      document.head.appendChild(st);

      // Count shield clicks directly, so a tap that lands can be told apart
      // from a tap that lands but fails to drive the player.
      window.__spy = { paused: 0, played: 0, state: 1, shieldClicks: 0 };
      document.addEventListener('click', e => {
        if (e.target && e.target.id === 'click-shield') window.__spy.shieldClicks++;
      }, true);

      window.YT = {
        PlayerState: { ENDED: 0, PLAYING: 1, PAUSED: 2 },
        Player: function (id, opts) {
          window.__spy.onStateChange = opts.events.onStateChange;
          this.destroy = function () {};
          this.getPlayerState = function () { return window.__spy.state; };
          this.getDuration = function () { return 300; };
          this.getCurrentTime = function () { return 12; };
          this.pauseVideo = function () { window.__spy.paused++; window.__spy.state = 2; };
          this.playVideo = function () { window.__spy.played++; window.__spy.state = 1; };
        }
      };
      // No fullscreen at all: this models the post-app-switch state, which is
      // exactly where the scroll-chaining is reachable.
      Object.defineProperty(document, 'fullscreenElement', { configurable: true, get: () => null });
      Element.prototype.requestFullscreen = function () { return Promise.reject(new Error('refused')); };
      document.exitFullscreen = function () { return Promise.resolve(); };
      window.fetch = () => Promise.resolve({ ok: true, text: () => Promise.resolve('') });
    });

    await p.addStyleTag({ path: 'static/style.css' });
    await p.addScriptTag({ path: 'static/app.js' });
    await p.evaluate(() => window.onYouTubeIframeAPIReady());

    // Control: the gesture scrolls a plain grid.
    await p.evaluate(() => window.scrollTo(0, 800));
    await p.waitForTimeout(300);
    const controlBefore = await p.evaluate(() => Math.round(window.scrollY));
    await drag(700, 300);
    const controlAfter = await p.evaluate(() => Math.round(window.scrollY));

    // Same gesture, player up, not fullscreen.
    await p.evaluate(() => window.scrollTo(0, 800));
    await p.waitForTimeout(300);
    await p.evaluate(() => document.querySelector('.grid-cell[data-video-id="v20"]').click());
    await p.waitForTimeout(500);
    const playerUp = await p.evaluate(() => ({
      hidden: document.getElementById('player-container').hidden,
      shield: !!document.getElementById('click-shield'),
      touchAction: getComputedStyle(document.getElementById('player-container')).touchAction
    }));
    const overlayBefore = await p.evaluate(() => Math.round(window.scrollY));
    await drag(700, 300);
    const overlayAfter = await p.evaluate(() => Math.round(window.scrollY));

    // Let the input pipeline settle before tapping, then alternate three times.
    await p.waitForTimeout(1200);
    const read = () => p.evaluate(() => ({
      paused: window.__spy.paused, played: window.__spy.played,
      state: window.__spy.state, shieldClicks: window.__spy.shieldClicks
    }));
    const taps = [];
    for (let i = 0; i < 3; i++) {
      await p.touchscreen.tap(200, 450);
      await p.waitForTimeout(1200);
      taps.push(await read());
    }

    return JSON.stringify({
      playerUp,
      control: { before: controlBefore, after: controlAfter },
      overlay: { before: overlayBefore, after: overlayAfter },
      taps,
      verdict: {
        rigCanScroll: controlAfter !== controlBefore,
        dragOnPlayerBlocked: overlayAfter === overlayBefore,
        everyTapLanded: taps[2].shieldClicks === 3,
        playbackAlternates: taps[0].paused === 1 && taps[1].played === 1 && taps[2].paused === 2
      }
    }, null, 2);
  } finally {
    await ctx.close();
  }
}
