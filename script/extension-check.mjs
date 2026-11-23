/* Loads a built, unpacked Chrome extension (make extension) in Playwright's
   Chromium, headless, and holds it to what a player meets: the toolbar button,
   the start screen, a press, a frame, the share link and a silent network.
   One line a check; the first failure ends the run with its reason. */
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';

// require, not import: it finds a playwright installed globally (NODE_PATH) too
const { chromium } = createRequire(import.meta.url)('playwright');

class Failed extends Error {}
const ok = function(line) { console.log('ok    ' + line); };
const fail = function(check, reason) { throw new Failed(check + ': ' + reason); };

const dir = path.resolve(process.argv[2] || '');
if (process.argv.length !== 3 || !fs.existsSync(path.join(dir, 'manifest.json'))) {
    console.error('usage: node script/extension-check.mjs <unpacked extension directory>');
    process.exit(2);
}
let manifest, sites;
try {
    manifest = JSON.parse(fs.readFileSync(path.join(dir, 'manifest.json'), 'utf8'));
    sites = (manifest.optional_host_permissions || []).map(function(pattern) { return new URL(pattern); });
} catch (err) {
    console.error('FAIL  the extension loads: manifest.json: ' + err.message);
    process.exit(1);
}

/* The browser's only way off the machine is this proxy, which carries nothing and
   keeps what was asked of it: Chromium's own calls home end here, and a request
   the extension made for its site would show. Loopback is not proxied. */
const asked = [];
const sink = http.createServer(function(req, res) {
    asked.push(req.url);
    res.writeHead(502).end();
});
sink.on('connect', function(req, socket) {
    asked.push(req.url);
    socket.end('HTTP/1.1 502 Bad Gateway\r\n\r\n');
});
await new Promise(function(resolve) { sink.listen(0, '127.0.0.1', resolve); });

const profile = fs.mkdtempSync(path.join(os.tmpdir(), 'vif-extension-check-'));
let context = null;
let status = 0;
try {
    context = await chromium.launchPersistentContext(profile, {
        channel: 'chromium',   // the full browser: its headless shell loads no extension
        headless: true,
        args: ['--disable-extensions-except=' + dir, '--load-extension=' + dir,
            '--proxy-server=http://127.0.0.1:' + sink.address().port],
    });
    await check(context);
} catch (err) {
    if (!(err instanceof Failed)) throw err;
    console.error('FAIL  ' + err.message);
    status = 1;
} finally {
    if (context) await context.close();
    sink.closeAllConnections();
    sink.close();
    try {
        // Chromium may still be writing as it goes: a profile left behind fails nothing
        fs.rmSync(profile, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
    } catch (err) {
        console.error('note  ' + profile + ' was not removed: ' + err.message);
    }
}
process.exit(status);

async function check(context) {
    // The service worker's address names the extension's id; no worker, no extension.
    let [worker] = context.serviceWorkers();
    if (!worker) {
        worker = await context.waitForEvent('serviceworker', { timeout: 20000 }).catch(function() {
            fail('the extension loads', 'Chromium registered no service worker for ' + dir + ' in 20 s');
        });
    }
    const root = new URL('/', worker.url()).href;
    ok('the extension loads: ' + manifest.name + ' ' + manifest.version + ' as ' + root);

    const requests = [];
    context.on('request', function(request) { requests.push(request.url()); });
    const tabs = function() {
        return context.pages().filter(function(p) { return p.url().startsWith(root); });
    };

    // The toolbar button, pressed as Chrome presses it: by the event its listener waits on.
    const press = function() {
        return worker.evaluate(function() { chrome.action.onClicked.dispatch({}); }).catch(function(err) {
            fail('the toolbar button opens the game', err.message.split('\n')[0]);
        });
    };
    await press();
    await until(function() { return tabs().length === 1; }, 10000) ||
        fail('the toolbar button opens the game', tabs().length + ' tabs of the extension are open, want 1');
    const page = tabs()[0];
    if (new URL(page.url()).pathname !== '/index.html') fail('the toolbar button opens the game', 'it opened ' + page.url());
    await press();
    await new Promise(function(resolve) { setTimeout(resolve, 1000); });
    if (tabs().length !== 1) fail('the toolbar button opens the game', 'a second press left ' + tabs().length + ' tabs, want the one');
    ok('the toolbar button opens the game in a tab, and a second press no other');

    // The checks below read what the page and its program's thread say and copy.
    const said = [];
    page.on('console', function(message) { said.push(message.text()); });
    page.on('pageerror', function(err) { said.push(err.message); });
    await page.addInitScript(function() {
        window.violations = [];
        document.addEventListener('securitypolicyviolation', function(e) {
            window.violations.push(e.violatedDirective + ' refused ' + (e.blockedURI || 'an inline script'));
        });
        navigator.clipboard.writeText = function(text) {
            window.copied = text;
            return Promise.resolve();
        };
    });
    const args = ['-seed=7'];
    await page.goto(root + 'index.html?arg=' + encodeURIComponent(args[0]));

    /* reaches waits for the page to stand at one of the stages wanted, and fails
       the check with the line the page shows when it does not: a run that ended,
       as one whose program Chromium refused to compile, says why there. */
    const reaches = async function(what, wanted, ms) {
        const stage = await page.waitForFunction(function(wanted) {
            const loading = document.getElementById('loading'), start = document.getElementById('start');
            const at = document.body.classList.contains('ended') ? 'ended'
                : loading.classList.contains('hidden') && start.hidden ? 'drawn'
                : /^Starting/.test(loading.textContent) ? 'started'
                : !start.hidden && loading.classList.contains('ask') ? 'asks' : 'loading';
            return at === 'ended' || wanted.includes(at) ? at : '';
        }, wanted, { timeout: ms }).then(function(handle) { return handle.jsonValue(); }, function() { return ''; });
        const line = await page.textContent('#loading');
        if (!wanted.includes(stage)) fail(what, (stage ? '' : 'after ' + ms / 1000 + ' s ') + 'the page reads "' + line + '"');
        return line;
    };

    const prompt = await reaches('the start screen asks for the start', ['asks'], 120000);
    ok('the start screen asks for the start: "' + prompt + '"');

    await page.keyboard.press('Enter');
    await reaches('a key press starts the game', ['started', 'drawn'], 10000);
    ok('a key press starts the game');

    await reaches('a frame is drawn', ['drawn'], 60000);
    const colors = await shown(page);
    if (colors < 3) fail('a frame is drawn', 'the canvas shows ' + colors + ' color(s)');
    ok('a frame is drawn: the canvas shows ' + colors + ' colors');

    const refused = (await page.evaluate(function() { return window.violations; }))
        .concat(said.filter(function(text) { return /Content Security Policy/i.test(text); }));
    if (refused.length) fail('no Content Security Policy violation', refused[0]);
    ok('no Content Security Policy violation');

    // The share button shows only behind a shared game; its click is the same here.
    await page.evaluate(function() { document.getElementById('share').click(); });
    const copied = await page.waitForFunction(function() { return window.copied; }, null, { timeout: 5000 })
        .then(function(handle) { return handle.jsonValue(); })
        .catch(function() { fail("the share link is the site's", 'the share button copied nothing'); });
    const link = new URL(copied);
    if (!sites.some(function(site) { return site.origin === link.origin; })) {
        fail("the share link is the site's", copied + ' is not at ' + sites.map(function(s) { return s.origin; }).join(' or '));
    }
    if (link.searchParams.getAll('arg').join('\n') !== args.join('\n')) {
        fail("the share link is the site's", copied + ' does not carry the page\'s arguments, ' + args.join(' '));
    }
    ok("the share link is the site's: " + copied);

    const strays = requests.filter(function(url) { return !url.startsWith(root); })
        .concat(asked.filter(function(target) {
            return sites.some(function(site) { return target.includes(site.hostname); });
        }));
    if (strays.length) fail('solo play asks nothing of a network', 'it asked for ' + strays[0]);
    ok('solo play asks nothing of a network: ' + requests.length + ' requests, each for a file of the extension');
}

// until polls a condition the browser gives no event for
async function until(done, ms) {
    for (const end = Date.now() + ms; Date.now() < end;) {
        if (done()) return true;
        await new Promise(function(resolve) { setTimeout(resolve, 100); });
    }
    return done();
}

/* shown counts the colors the game's canvas shows, in a screenshot read back in
   the page: a WebGL canvas keeps no pixels a script could read, and a canvas
   nothing drew on is one color. */
async function shown(page) {
    const shot = await page.locator('#screen').screenshot();
    return page.evaluate(async function(base64) {
        const bytes = Uint8Array.from(atob(base64), function(c) { return c.charCodeAt(0); });
        const image = await createImageBitmap(new Blob([bytes], { type: 'image/png' }));
        const canvas = new OffscreenCanvas(image.width, image.height);
        const drawn = canvas.getContext('2d');
        drawn.drawImage(image, 0, 0);
        const pixels = new Uint32Array(drawn.getImageData(0, 0, image.width, image.height).data.buffer);
        return new Set(pixels).size;
    }, shot.toString('base64'));
}
