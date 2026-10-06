// Smoke test on the real page: real server-rendered grid, real thumbnails,
// real YouTube player. Only app.js/style.css are swapped for the local copies.
// Leaving a real video must leave the real feed exactly as it was.
async (page) => {
  await page.unrouteAll();
  await page.route('**/static/app.js', r => r.abort());
  await page.route('**/static/style.css', r => r.abort());
  // Never write test plays into the family's real history.
  await page.route('**/history', r => r.abort());

  await page.goto('https://refsnes-barnetv.no/');
  await page.addStyleTag({ path: 'static/style.css' });
  await page.addScriptTag({ path: 'static/app.js' });
  await page.waitForFunction(() => window.YT && window.YT.Player);
  await page.evaluate(() => window.onYouTubeIframeAPIReady());

  const snapshot = () => page.evaluate(() => {
    const pc = document.getElementById('player-container');
    return {
      cells: document.querySelectorAll('.grid-cell').length,
      firstIds: Array.from(document.querySelectorAll('.grid-cell')).slice(0, 5).map(c => c.dataset.videoId),
      scrollY: Math.round(window.scrollY),
      playerHidden: pc.hidden,
      iframes: pc.querySelectorAll('iframe').length,
      gridVisible: getComputedStyle(document.querySelector('.grid')).display !== 'none'
    };
  });

  await page.evaluate(() => window.scrollTo(0, 600));
  await page.waitForTimeout(300);
  const before = await snapshot();

  await page.evaluate(() => document.querySelectorAll('.grid-cell')[4].click());
  await page.waitForFunction(
    () => document.querySelector('#player-container iframe') !== null, null, { timeout: 15000 });
  await page.waitForTimeout(3000);
  const during = await snapshot();
  const fullscreened = await page.evaluate(() => document.fullscreenElement !== null);

  // A synthetic click is not a trusted gesture, so requestFullscreen may have
  // been refused. Either way, model what Back does: fullscreen goes away while
  // the page is in the foreground.
  await page.evaluate(() => {
    if (document.fullscreenElement) return document.exitFullscreen();
    document.dispatchEvent(new Event('fullscreenchange'));
  });
  await page.waitForTimeout(2500);
  const after = await snapshot();

  return JSON.stringify({
    before, during, fullscreened, after,
    verdict: {
      playerReallyOpened: during.iframes === 1 && during.playerHidden === false,
      sameTiles: JSON.stringify(after.firstIds) === JSON.stringify(before.firstIds),
      sameScroll: after.scrollY === before.scrollY,
      feedNotReloaded: after.cells >= before.cells,
      playerTornDown: after.playerHidden === true && after.iframes === 0,
      gridVisible: after.gridVisible === true
    }
  }, null, 2);
}
