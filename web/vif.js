(function() {
    'use strict';

    /* The files beside the page, by the names web/ gives them. A site that
       publishes them under other names, as content-hashed ones, says so on this
       script's element: data-wasm, data-worker, data-exec (wasm_exec.js) and
       data-audio (audio-worklet.js). */
    const files = (document.currentScript && document.currentScript.dataset) || {};
    const WASM_PATH = files.wasm || 'vif.wasm';
    const WORKER_PATH = files.worker || 'worker.js';
    const EXEC_PATH = files.exec || 'wasm_exec.js';
    const AUDIO_PATH = files.audio || 'audio-worklet.js';

    /* The screen, the keys and the pointer are TerminalPage's, the terminal module's
       script ahead of this one in terminal.js. A cell opens CELL CSS pixels tall and
       half as wide, the aspect the game is authored at; a phone starts at 6, which
       holds the map's width in its screen. A zoom moves it within the range Go
       names, Android's too, and Go keeps the cell it leaves. */
    const touch = TerminalPage.touch;
    const CELL = touch && Math.min(screen.width, screen.height) < 600 ? 6 : 16;

    const input = document.getElementById('input');
    let page = null;   // TerminalPage's page, once open: the program is reached through it

    // === Start screen ===
    /* Every page opens on Android's start screen: the logo, the loading line, which
       asks for the start once the program is compiled, and ? for the controls. The
       Worker holds that program until one tap, click or key press, made at any point
       of the download or compile; the press opens the sound too, as a context made
       inside one plays from the first buffer. */
    const start = document.getElementById('start');
    const help = document.getElementById('help');
    const PROMPT = touch ? 'Tap to start' : 'Click or press a key to start';
    // The named keys that start the game: Escape, Tab and the F keys stay the
    // browser's and the screen's, as do chords
    const START_KEYS = /^(Enter|Backspace|Delete|Arrow(Up|Down|Left|Right)|Home|End|Page(Up|Down)|Insert)$/;
    let ready = false;      // the program is compiled and held for the start
    let tapped = false;     // the tap, click or key press came
    let startCode = null;   // the start key held down, which starts the game as it comes up

    function wireStart() {
        const controls = document.getElementById('controls');
        const title = touch ? 'Touch controls' : 'Mouse and keyboard';
        document.getElementById('help-title').textContent = title;
        document.getElementById(touch ? 'help-touch' : 'help-mouse').hidden = false;
        controls.setAttribute('aria-label', title);
        start.hidden = false;
        // A tap here leaves focus where it is: on the field, once the program loaded.
        start.addEventListener('pointerdown', function(e) { e.preventDefault(); });
        // Anywhere but a button starts the game, or returns to the one kept behind.
        start.addEventListener('click', function(e) {
            if (e.target.closest('button')) return;
            if (away) resume(); else begin();
        });
        document.addEventListener('keydown', startKey, true);
        document.addEventListener('keyup', startKey, true);
        document.getElementById('menu-reload').addEventListener('click', function() { location.reload(); });
        document.getElementById('menu-solo').addEventListener('click', playSolo);
        document.getElementById('share').addEventListener('click', share);
        // The ? sits nearer the edge than the window, and would show past its corner.
        controls.addEventListener('click', function() { controls.hidden = true; help.showModal(); });
        help.addEventListener('close', function() { controls.hidden = false; });
        document.getElementById('help-close').addEventListener('click', function() { help.close(); });
        help.addEventListener('click', function(e) { if (e.target === help) help.close(); });   // the backdrop
    }

    /* A key starts the game as a click does, on its release, and the game sees
       none of it: neither its press nor its repeats while held. Keys are the
       window's while it is open and the ?'s while it has focus. Back from a game,
       a key returns to it the same way. */
    function startKey(e) {
        if ((tapped && !away) || help.open || e.target.closest('button')) return;
        if (e.type === 'keyup') {
            if (e.code !== startCode) return;
        } else if (e.isComposing || e.ctrlKey || e.altKey || e.metaKey
            || ([...e.key].length !== 1 && !START_KEYS.test(e.key))) {
            return;
        }
        e.preventDefault();
        e.stopPropagation();
        if (e.type === 'keyup') {
            if (away) resume(); else begin();
        } else {
            startCode = e.code;
            if (!audio) openAudio();   // in the press: a release lets no sound start on some browsers
        }
    }

    // begin is the start screen's tap, click or key: the program runs from it, now
    // or once compiled. The loading line turns the button it asks with from then.
    function begin() {
        if (tapped) return;
        tapped = true;
        document.getElementById('loading').classList.add('ask');
        page.focus();   // keys follow, in a frame that had no focus too
        if (!audio) openAudio();
        if (ready) run();
    }

    function run() {
        if (!page.running) return;
        page.run();
        document.getElementById('loading').textContent = 'Starting…';
    }

    /* Launch arguments, from the page and from the query string. This is how a
       session reaches the build: the fleet page hands out ?arg=-join=<ws url>,
       and nothing here constructs or validates that URL — the allocator issued
       it and the browser's own CSP decides whether it may be opened. */
    function launchArguments() {
        const pageArgs = Array.isArray(window.VIF_ARGS) ? window.VIF_ARGS : [];
        const queryArgs = new URLSearchParams(window.location.search).getAll('arg');
        const args = pageArgs.concat(queryArgs);

        if (args.length > 64 || args.some(arg => typeof arg !== 'string' || arg.length > 1024)) {
            throw new Error('invalid vif launch arguments');
        }
        return ['vif'].concat(args);
    }

    /* A browser has no vif.toml: the settings a run writes back are kept in this
       site's storage, so the next visit starts as this one was left. A private
       window or a site whose storage is blocked refuses them; they last the run. */
    const SETTINGS_KEY = 'vif.settings';

    function keptSettings() {
        try {
            return localStorage.getItem(SETTINGS_KEY) || '';
        } catch (err) {
            return '';
        }
    }

    function keepSettings(text) {
        try {
            localStorage.setItem(SETTINGS_KEY, text);
        } catch (err) {
            notice('Settings not saved: ' + err.message.replace(/\.$/, '') + '.');
        }
    }

    /* The places a run comes back to its session with. The first tab on this site
       holds the site's places (localStorage) under a lock released when it closes, so
       a closed tab is come back to from a new one, as a terminal comes back to
       places.json; a tab beside it, and every tab where locks are not offered, is
       another player with places of its own (sessionStorage). Refused, they last the run. */
    const PLACES_KEY = 'vif.places';
    const PLACES_LOCK = 'vif.places';
    let placesStorage = null;

    function storageOf(name) {
        try {
            return window[name] || null;
        } catch (err) {
            return null;
        }
    }

    function claimPlaces() {
        return new Promise(function(resolve) {
            const settle = function(storage) {
                if (placesStorage === null) placesStorage = storage || storageOf('sessionStorage');
                resolve();
            };
            if (!navigator.locks || typeof navigator.locks.request !== 'function') {
                settle(null);
                return;
            }
            navigator.locks.request(PLACES_LOCK, { ifAvailable: true }, function(lock) {
                if (!lock) {
                    settle(null);
                    return undefined;
                }
                settle(storageOf('localStorage'));
                return new Promise(function() {}); /* held until the tab closes */
            }).catch(function() { settle(null); });
        });
    }

    function keptPlaces() {
        try {
            return (placesStorage && placesStorage.getItem(PLACES_KEY)) || '';
        } catch (err) {
            return '';
        }
    }

    function keepPlaces(text) {
        try {
            if (placesStorage) placesStorage.setItem(PLACES_KEY, text);
        } catch (err) {
            /* nothing to tell: the run keeps them */
        }
    }

    function showExit(code, reason) {
        if (code === 0) {
            showMessage('vif has exited.', false);
            return;
        }
        reason = (reason || 'exit code ' + code).replace(/\.$/, '');
        showMessage('vif stopped: ' + reason + '.', true);
    }

    // === WASM Loading ===
    const MB = 1048576; // 1024 * 1024 bytes in 1 Megabyte
    const fmtProgress = (received, total) => total
        ? (received / MB).toFixed(1) + ' / ' + (total / MB).toFixed(1) + ' MB'
        : (received / MB).toFixed(1) + ' MB';

    // What kept the program from loading or starting, worded by the stage TerminalPage names
    const FAILED = {
        wasm: function(reason) { return reason + '.'; },
        canvas: function(reason) { return 'vif needs a canvas, which this browser does not provide (' + reason + ').'; },
        load: function(reason) { return 'Failed to load vif (' + reason + ').'; },
        start: function(reason) { return 'Failed to start vif (' + reason + ').'; },
    };

    /* A joined page hidden this long leaves its session: its Worker is ended, so the
       session sees the player go and one nobody else plays ends on its grace. Shown
       again after that long, ended here or frozen by the browser, the page reloads,
       which rejoins with the tab's claim while the place waits. A page that only
       spectates holds no place, so it goes sooner: a feed nobody looks at is waste. */
    const LEAVE_HIDDEN_MS = 5 * 60 * 1000, SPECTATE_HIDDEN_MS = 30 * 1000;
    let hiddenAt = 0, leaveTimer = 0, left = false;

    function leave() {
        if (!document.hidden || !page.running) return;
        left = true;
        page.stop();
        closeAudio();
    }

    document.addEventListener('visibilitychange', function() {
        if (document.hidden) {
            if (joined && playing && page.running && !hiddenAt) {
                hiddenAt = Date.now();
                leaveTimer = setTimeout(leave, hiddenLimit);
            }
            return;
        }
        clearTimeout(leaveTimer);
        if (left || hiddenAt && Date.now() - hiddenAt >= hiddenLimit) location.reload();
        hiddenAt = 0;
    });

    // === Sound ===
    /* The Worker posts the mixer's PCM once a sound plays; a muted game posts
       nothing. The first buffer opens an AudioContext at the mixer's
       rate and loads the worklet (AUDIO_PATH) that plays what follows through a ring.
       A context opened outside a gesture stays suspended, so each key or pointer
       resumes it; a buffer that arrives before it runs is dropped. */
    let audio = null;   // { ctx, node }, node once the worklet loaded

    function onAudio(data) {
        if (!audio) openAudio();
        if (audio.node && audio.ctx.state === 'running') audio.node.port.postMessage(data, [data.buffer]);
    }

    function openAudio() {
        audio = { ctx: null, node: null };
        let ctx;
        try {
            ctx = new AudioContext({ sampleRate: 44100 });
        } catch (err) {
            return;   // no sound on this browser; the game plays on
        }
        if (!ctx.audioWorklet) {
            ctx.close().catch(function() {});
            return;
        }
        audio.ctx = ctx;
        ctx.audioWorklet.addModule(AUDIO_PATH).then(function() {
            audio.node = new AudioWorkletNode(ctx, 'vif-speaker', { numberOfInputs: 0, outputChannelCount: [2] });
            audio.node.connect(ctx.destination);
        }).catch(function(err) {
            notice('No sound (' + err.message + ').');
        });
        // The events that count as a gesture: a mouse's press, a touch's release, a key.
        const wake = function() { if (ctx.state !== 'running') ctx.resume().catch(function() {}); };
        for (const type of ['pointerdown', 'pointerup', 'keydown']) document.addEventListener(type, wake, true);
        wake();
    }

    // An ended program's sound ends with it
    function closeAudio() {
        if (audio && audio.ctx) audio.ctx.close().catch(function() {});
    }

    // === Key strip ===
    /* A touch screen's strip: its keys (data-key) are TerminalPage's to send, its
       keyboard button raises or drops the system keyboard, as three taps on the status
       bar do, and its fullscreen button shows where the browser has an API for it,
       which iPhone Safari has not. */
    function wireStrip() {
        const fullButton = document.getElementById('full');
        document.getElementById('kbd').addEventListener('click', function() { page.toggleKeyboard(); });
        if (!document.fullscreenEnabled) return;
        fullButton.hidden = false;
        fullButton.addEventListener('click', toggleFullscreen);
        document.addEventListener('fullscreenchange', function() {
            fullButton.setAttribute('aria-pressed', document.fullscreenElement !== null);
        });
        function toggleFullscreen() {
            const done = document.fullscreenElement
                ? document.exitFullscreen()
                : document.documentElement.requestFullscreen({ navigationUI: 'hide' });
            done.catch(function() {});
        }
    }

    // keyboardShown follows the system keyboard asked for or dropped: Esc is at hand while typing
    function keyboardShown(up) {
        document.getElementById('kbd').setAttribute('aria-pressed', up);
        document.getElementById('esc').hidden = !up;
    }

    // notice shows a line at the top for a while, over a game that goes on
    function notice(msg) {
        const el = document.getElementById('notice');
        el.textContent = msg;
        el.hidden = false;
        setTimeout(function() { el.hidden = true; }, 5000);
    }

    // Every message here ends the run: it takes the place of the start screen and
    // the game, with a reload, and a solo game in place of a shared one that
    // refused or ended
    function showMessage(msg, isError) {
        const el = document.getElementById('loading');
        el.textContent = msg;
        el.classList.remove('hidden', 'ask');
        el.classList.toggle('error', isError);
        start.hidden = true;
        document.body.classList.add('ended');
        away = false;
        const actions = document.createElement('div');
        actions.className = 'actions';
        const again = button('Reload', function() { location.reload(); });
        actions.append(again);
        if (joined && isError) actions.append(button('Play solo', playSolo));
        el.append(actions);
        again.focus({ preventScroll: true });
    }

    function button(label, onClick) {
        const b = document.createElement('button');
        b.type = 'button';
        b.textContent = label;
        b.addEventListener('click', onClick);
        return b;
    }

    // === Back ===
    /* Back is the game's Escape, as on Android, which in Normal opens the menu, and
       a second back the start screen again over the game kept as it was, the menu
       closed on the way: a solo game waits for its player (away), a shared one goes
       on. A tap or key there returns to it, and back from there leaves the page. A
       browser passes over entries pushed without a gesture, so one press pushes both. */
    const sessionArg = /^-(join|spectate)(=|$)/;   // a session played in, or only spectated
    const launched = (function() {
        try {
            return launchArguments().slice(1);
        } catch (err) {
            return [];   // refused as the load starts, which says so
        }
    })();
    const joined = launched.some(function(arg) { return sessionArg.test(arg); });
    const hiddenLimit = launched.some(function(arg) { return /^-spectate(=|$)/.test(arg); })
        ? SPECTATE_HIDDEN_MS : LEAVE_HIDDEN_MS;
    let playing = false;   // the first frame came
    let away = false;      // the start screen is up over the game
    let traps = 0;         // entries above the page's own for back to land on

    function arm() {
        if (!playing || away || !page.running) return;
        while (traps < 2) history.pushState({ vif: ++traps }, '');
    }

    window.addEventListener('popstate', function(e) {
        traps = (e.state && e.state.vif) || 0;
        if (away) return;
        if (!playing || !page.running) {   // nothing to keep: back leaves, as it would have
            history.back();
            return;
        }
        if (traps) page.send({ type: 'key', key: 'Escape', mods: 0 });
        else showAway();
    });

    function showAway() {
        away = true;
        // Its close goes before the away, which the run takes after it, so the pause
        // the menu held is the away's and not ended behind the screen
        if (menuJSON) {
            closeMenu();
            menuOp('close', '', '');
        }
        page.lost();
        page.send({ type: 'blur' });
        page.send({ type: 'away', away: true });
        page.setHidden(true);   // nothing drawn under it
        input.blur();
        document.getElementById('menu-line').textContent = touch
            ? 'Tap to return to the game' : 'Click or press a key to return to the game';
        // Reload is at hand here: some browsers keep this page from being pulled down to reload
        document.getElementById('menu-solo').hidden = !joined;
        document.getElementById('share').hidden = !joined;
        document.getElementById('menu').hidden = false;
        start.classList.add('menu');
        start.hidden = false;
    }

    function resume() {
        if (!away || !page.running) return;
        away = false;
        start.hidden = true;
        start.classList.remove('menu');
        document.getElementById('menu').hidden = true;
        document.getElementById('share').hidden = true;
        page.send({ type: 'away', away: false });
        page.setHidden(false);
        page.focus();
        arm();
    }

    // playSolo opens this page without its session, in its place: the same build alone
    function playSolo() {
        const url = new URL(location.href);
        const args = url.searchParams.getAll('arg');
        url.searchParams.delete('arg');
        for (let i = 0; i < args.length; i++) {
            const session = sessionArg.exec(args[i]);
            if (!session) {
                url.searchParams.append('arg', args[i]);
                continue;
            }
            if (!session[2]) i++;   // the address as the next argument
            // The slot a spectator looks from may follow its address
            if (session[1] === 'spectate' && /^[0-9a-fA-F]{1,2}$/.test(args[i + 1] || '')) i++;
        }
        url.hash = '';
        location.replace(url.href);
    }

    // share copies this page's address, which opens the same game as this page did
    let shareTimer = 0;
    function share() {
        const done = document.getElementById('share-done');
        const say = function(text) {
            done.textContent = text;
            done.hidden = false;
            clearTimeout(shareTimer);
            shareTimer = setTimeout(function() { done.hidden = true; }, 1500);
        };
        const url = location.href.split('#')[0];
        if (!navigator.clipboard) {
            say('Copy needs a secure page');
            return;
        }
        navigator.clipboard.writeText(url).then(function() { say('Link copied'); },
            function() { say('Copy refused'); });
    }

    // === Configuration menu ===
    /* The run's menu, drawn here from its snapshot as Android's app draws it
       (doc/native-menu.md): a page is a heading whose rows open under it, each kind
       of row its own control, and a tap sends the run an op by the node's id. A
       snapshot rewrites the rows in place, so expansion, scroll and focus stay. A Go
       test holds this file to the run's contract: its node kinds, ops and field inputs. */
    const MENU_KINDS = ['group', 'toggle', 'choice', 'step', 'pick', 'action', 'form', 'info'];
    const MENU_OPS = ['set', 'step', 'activate', 'submit', 'search', 'close'];
    const MENU_INPUTS = ['text', 'number', 'uri'];
    const config = document.getElementById('config');
    const configBody = document.getElementById('config-body');
    const configSearch = document.getElementById('config-search');
    const configStatus = document.getElementById('config-status');
    const expanded = new Set();   // the pages open, kept while the page lives
    const menuRows = new Map();   // the rows shown, by node id
    let menuJSON = '';            // the snapshot applied, which an expansion applies again
    let menuQuery = '';           // the search the run holds
    let sheet = null;             // the confirm, radio list or form up over the menu

    function menuOp(op, id, arg) {
        if (MENU_OPS.includes(op)) page.send({ type: 'menu', op: op, id: id || '', arg: arg || '' });
    }

    // applyMenu draws the menu as the run has it; an empty snapshot closes it
    function applyMenu(json) {
        if (!json) {
            closeMenu();
            return;
        }
        let snap;
        try {
            snap = JSON.parse(json);
        } catch (err) {
            console.error('vif: menu snapshot', err);
            return;
        }
        menuJSON = json;
        document.getElementById('config-title').textContent = snap.title || 'Menu';
        const line = snap.status;
        configStatus.hidden = !line;
        if (line) {
            configStatus.textContent = line.text;
            configStatus.className = line.kind || '';
        }
        const nodes = new Map();
        indexNodes(snap.nodes, nodes);
        const wanted = [];
        menuQuery = snap.query || '';
        if (menuQuery) {
            for (const f of snap.found || []) {
                const n = nodes.get(f.id);
                if (n) wanted.push({ node: n, label: f.label, depth: 1 });
            }
            if (!wanted.length) wanted.push({ node: { id: '#none', kind: 'info', label: 'Nothing matches' }, label: 'Nothing matches', depth: 1 });
        } else {
            walkNodes(snap.nodes, 0, wanted);
            // The run dropped the search, as Escape does: the field follows, unless it is being typed in
            if (configSearch.value && document.activeElement !== configSearch) configSearch.value = '';
        }
        reconcileRows(wanted);
        const form = snap.form || {};
        for (const row of menuRows.values()) row.error(row.id === form.id ? form.error : '');
        if (sheet) {
            if (sheet.error) sheet.error.textContent = sheet.id === form.id ? form.error : '';
            if (!nodes.has(sheet.id)) closeSheet();   // the row it was up for is gone: a bot that left
        }
        if (!config.open) {
            config.showModal();
            page.lost();
            page.send({ type: 'blur' });
        }
    }

    function indexNodes(nodes, into) {
        for (const n of nodes || []) {
            into.set(n.id, n);
            indexNodes(n.nodes, into);
        }
    }

    // walkNodes lists the rows to show: every page, the rows of the open ones, and a
    // section as a title over its own rows
    function walkNodes(nodes, depth, into) {
        for (const n of nodes || []) {
            into.push({ node: n, label: n.label, depth: depth });
            if (n.kind === 'group' && (depth > 0 || expanded.has(n.id))) walkNodes(n.nodes, depth + 1, into);
        }
    }

    // reconcileRows makes the rows shown the entries wanted, in order, keeping every
    // row whose node stands so its state stays
    function reconcileRows(wanted) {
        for (let i = 0; i < wanted.length; i++) {
            const e = wanted[i], id = e.node.id;
            let row = menuRows.get(id);
            if (row && (row.kind !== e.node.kind || row.depth !== e.depth)) {
                row.el.remove();
                menuRows.delete(id);
                row = null;
            }
            if (!row) {
                row = makeRow(e);
                menuRows.set(id, row);
            }
            if (configBody.children[i] !== row.el) configBody.insertBefore(row.el, configBody.children[i] || null);
            row.update(e);
        }
        while (configBody.children.length > wanted.length) {
            const el = configBody.lastElementChild;
            menuRows.delete(el.dataset.id);
            el.remove();
        }
    }

    // nodeOf is a node of the snapshot applied last, by id
    function nodeOf(id) {
        const nodes = new Map();
        try {
            indexNodes(JSON.parse(menuJSON).nodes, nodes);
        } catch (err) {
            return {};
        }
        return nodes.get(id) || {};
    }

    function span(cls, text) {
        const el = document.createElement('span');
        el.className = cls;
        if (text) el.textContent = text;
        return el;
    }

    /* makeRow builds a node's row: a label over its description, and the control its
       kind takes, which shows the snapshot's value and sends an op when touched. A
       page is a heading with a chevron that opens its rows under it; a section is
       a title. */
    function makeRow(e) {
        const n = e.node, kind = n.kind, depth = e.depth, id = n.id;
        const row = { id: id, kind: kind, depth: depth, choices: [], fields: [], disabled: false };
        const el = document.createElement('div');
        const page = kind === 'group' && depth === 0, section = kind === 'group' && depth > 0;
        el.className = 'row ' + (page ? 'page' : section ? 'section' : kind) + (depth > 0 ? ' in' : '');
        el.dataset.id = id;
        row.el = el;
        const text = span('text'), label = span('label'), detail = span('detail');
        text.append(label, detail);
        let value = null, control = null, trailing = null, field = null, error = null;
        const actable = function() {
            el.classList.add('act');
            el.tabIndex = 0;
            el.setAttribute('role', 'button');
            el.addEventListener('click', function(ev) {
                if (ev.target.closest('select, .field, .stepper')) return;
                row.act();
            });
        };
        if (page) {
            trailing = span('chevron');
            actable();
        }
        switch (kind) {
        case 'toggle':
            control = document.createElement('input');
            control.type = 'checkbox';
            control.className = 'switch';
            control.setAttribute('role', 'switch');
            control.tabIndex = -1;
            control.addEventListener('click', function(ev) { ev.preventDefault(); });   // controlled: the snapshot sets it
            actable();
            break;
        case 'choice':
            control = document.createElement('select');
            control.className = 'choice';
            control.addEventListener('change', function() {
                menuOp('set', id, control.value);
                control.value = nodeOf(id).value || '';
            });
            break;
        case 'step':
            value = span('value');
            el.tabIndex = 0;
            break;
        case 'pick':
            control = document.createElement('input');
            control.type = 'radio';
            control.className = 'radio';
            control.tabIndex = -1;
            control.addEventListener('click', function(ev) { ev.preventDefault(); });
            value = span('note');
            actable();
            break;
        case 'action':
            trailing = span('arrow', '›');
            actable();
            break;
        case 'form':
            row.fields = n.fields || [];
            if (row.fields.length === 1) {
                field = fieldFor(row.fields[0]);
                field.value = row.fields[0].value || '';
                field.placeholder = row.fields[0].label || '';
                field.setAttribute('enterkeyhint', 'go');
                field.addEventListener('keydown', function(ev) {
                    if (ev.key === 'Enter') {
                        ev.preventDefault();
                        menuOp('submit', id, JSON.stringify([field.value]));
                    }
                });
                error = span('error');
                text.insertBefore(field, detail);
                text.append(error);
            } else {
                el.classList.add('sheet');
                trailing = span('arrow', '›');
                actable();
            }
            break;
        default:
            break;
        }
        if (kind === 'pick') el.append(control);
        el.append(text);
        if (kind === 'step') {
            el.append(stepper('−', -1), value, stepper('+', 1));
        } else {
            for (const w of [value, kind === 'pick' ? null : control, trailing]) {
                if (w) el.append(w);
            }
        }

        function stepper(sign, direction) {
            const b = document.createElement('button');
            b.type = 'button';
            b.className = 'stepper';
            b.textContent = sign;
            b.setAttribute('aria-label', direction < 0 ? 'Less' : 'More');
            b.addEventListener('click', function() { row.turn(direction); });
            return b;
        }

        // update rewrites the row from a snapshot's node; what was typed into a field stays
        row.update = function(entry) {
            const node = entry.node;
            label.textContent = entry.label;
            const reason = node.disabled || '', description = node.description || '';
            row.disabled = reason !== '';
            detail.textContent = row.disabled && description ? reason + '. ' + description : row.disabled ? reason : description;
            el.classList.toggle('off', row.disabled);
            row.choices = node.choices || [];
            const shown = node.value || '';
            switch (kind) {
            case 'group':
                if (trailing) {
                    const open = expanded.has(id);
                    trailing.textContent = open ? '▾' : '▸';
                    el.setAttribute('aria-expanded', open);
                }
                return;
            case 'toggle':
                control.checked = row.choices.length === 2 && shown === row.choices[1];
                control.disabled = row.disabled;
                break;
            case 'choice':
                while (control.options.length) control.remove(0);
                for (const c of row.choices) {
                    const o = document.createElement('option');
                    o.value = c;
                    o.textContent = c;
                    control.append(o);
                }
                control.value = shown;
                control.disabled = row.disabled;
                break;
            case 'step':
                value.textContent = shown;
                break;
            case 'pick':
                control.checked = !!node.marked;
                value.textContent = shown;
                break;
            default:
                break;
            }
            if (field) field.disabled = row.disabled;
            let said = entry.label;
            if (shown && kind !== 'form') said += ', ' + shown;
            el.setAttribute('aria-label', row.disabled ? said + ', unavailable: ' + reason : said);
        };

        // error is what a submit of this form left, shown under its field
        row.error = function(message) {
            if (error) error.textContent = message || '';
        };

        // act is a tap on the row, or Enter on it
        row.act = function() {
            switch (kind) {
            case 'group':
                if (!expanded.delete(id)) expanded.add(id);
                applyMenu(menuJSON);
                break;
            case 'toggle':
                if (!row.disabled && row.choices.length === 2) menuOp('set', id, control.checked ? row.choices[0] : row.choices[1]);
                break;
            case 'pick':
                if (!row.disabled) menuOp('activate', id, '');
                break;
            case 'action':
                if (row.disabled) break;
                if (nodeOf(id).confirm) confirmSheet(id, entry());
                else menuOp('activate', id, '');
                break;
            case 'form':
                if (row.disabled) break;
                if (field) field.focus();
                else formSheet(id, nodeOf(id));
                break;
            default:
                break;
            }
        };

        // turn is h or l, or the steppers: a step moves, a toggle turns, a choice cycles
        row.turn = function(direction) {
            if (row.disabled) return;
            switch (kind) {
            case 'step':
                menuOp('step', id, direction > 0 ? '+1' : '-1');
                break;
            case 'toggle':
                if (row.choices.length === 2) menuOp('set', id, row.choices[direction > 0 ? 1 : 0]);
                break;
            case 'choice': {
                const i = row.choices.indexOf(nodeOf(id).value || '');
                if (row.choices.length) menuOp('set', id, row.choices[((i + direction) % row.choices.length + row.choices.length) % row.choices.length]);
                break;
            }
            case 'pick':
                row.act();
                break;
            default:
                break;
            }
        };

        function entry() { return { label: label.textContent, node: nodeOf(id) }; }
        return row;
    }

    // fieldFor is a field taking what its input names: numbers, an address or text
    function fieldFor(f) {
        const el = document.createElement('input');
        el.type = 'text';
        el.className = 'field';
        el.autocomplete = 'off';
        el.spellcheck = false;
        const kind = MENU_INPUTS.includes(f.input) ? f.input : 'text';
        el.inputMode = kind === 'number' ? 'numeric' : kind === 'uri' ? 'url' : 'text';
        return el;
    }

    // openSheet opens the window the picker, the confirm and a form share, titled,
    // and returns it; closing it forgets it
    function openSheet(id, heading) {
        closeSheet();
        const d = document.createElement('dialog');
        d.className = 'sheet';
        const h = document.createElement('h2');
        h.textContent = heading;
        d.append(h);
        d.addEventListener('click', function(ev) { if (ev.target === d) d.close(); });   // the backdrop
        d.addEventListener('close', function() {
            if (sheet && sheet.el === d) sheet = null;
            d.remove();
            configBody.focus({ preventScroll: true });
        });
        document.body.append(d);
        sheet = { el: d, id: id, error: null };
        return d;
    }

    function closeSheet() {
        if (sheet && sheet.el.open) sheet.el.close();
        sheet = null;
    }

    function about(d, node, pad) {
        const p = document.createElement('p');
        p.className = 'about';
        p.textContent = node.description || '';
        if (pad) p.style.paddingBottom = pad;
        d.append(p);
    }

    // sheetButtons is Cancel beside the action named, the site's ring and fill
    function sheetButtons(d, action, onAction) {
        const row = document.createElement('div');
        row.className = 'actions';
        const cancel = button('Cancel', function() { d.close(); });
        const go = button(action, onAction);
        go.className = 'go';
        row.append(cancel, go);
        d.append(row);
    }

    // The question before a destructive action: its description, Cancel and the action
    function confirmSheet(id, e) {
        const d = openSheet(id, e.label);
        about(d, e.node, '8px');
        sheetButtons(d, e.label, function() {
            menuOp('activate', id, '');
            d.close();
        });
        d.showModal();
    }

    // The fields of a longer form, the focused one's help under it, the error above Submit
    function formSheet(id, node) {
        const d = openSheet(id, node.label || '');
        about(d, node);
        const inputs = [];
        for (const f of node.fields || []) {
            const name = span('name', f.label);
            const el = fieldFor(f);
            el.value = f.value || '';
            const help = document.createElement('p');
            help.className = 'help';
            help.textContent = f.help || '';
            help.hidden = true;
            el.addEventListener('focus', function() { help.hidden = !f.help; });
            el.addEventListener('blur', function() { help.hidden = true; });
            d.append(name, el, help);
            inputs.push(el);
        }
        const error = span('error');
        d.append(error);
        sheet.error = error;
        const submit = function() { menuOp('submit', id, JSON.stringify(inputs.map(function(i) { return i.value; }))); };
        for (let i = 0; i < inputs.length; i++) {
            inputs[i].setAttribute('enterkeyhint', i === inputs.length - 1 ? 'done' : 'next');
            inputs[i].addEventListener('keydown', function(ev) {
                if (ev.key !== 'Enter') return;
                ev.preventDefault();
                if (i < inputs.length - 1) inputs[i + 1].focus(); else submit();
            });
        }
        sheetButtons(d, 'Submit', submit);
        d.showModal();
        if (inputs.length) inputs[0].focus();
    }

    // back closes the innermost of confirm, picker and form, then clears a search, then sends close
    function back() {
        if (sheet) closeSheet();
        else if (configSearch.value || menuQuery) {
            configSearch.value = '';
            menuOp('search', '', '');
        } else {
            menuOp('close', '', '');
        }
    }

    // closeMenu is the run's word that the menu closed: the window goes, whatever is up
    // with it, and the keys go back to the game unless its player is away from it
    function closeMenu() {
        menuJSON = '';
        closeSheet();
        if (config.open) config.close();
        if (!away) page.focus();
    }

    function rowOf(el) {
        const r = el && el.closest ? el.closest('.row') : null;
        return r ? menuRows.get(r.dataset.id) : null;
    }

    function wireMenu() {
        document.getElementById('config-close').addEventListener('click', function() { menuOp('close', '', ''); });
        config.addEventListener('click', function(ev) { if (ev.target === config) back(); });   // the backdrop
        // Escape asks to close: Back's order instead. A browser may close the window
        // regardless, and the run is told then, so it never holds a menu nobody sees.
        config.addEventListener('cancel', function(ev) {
            ev.preventDefault();
            back();
        });
        // A held Escape acts once, as in the run: its repeats would close what its press
        // left open, the menu it opened or the one under a sheet it closed
        document.addEventListener('keydown', function(ev) {
            if (ev.key === 'Escape' && ev.repeat && (config.open || sheet)) ev.preventDefault();
        }, true);
        config.addEventListener('close', function() {
            if (menuJSON) {
                menuJSON = '';
                closeSheet();
                menuOp('close', '', '');
                page.focus();
            }
        });
        configSearch.addEventListener('input', function() { menuOp('search', '', configSearch.value); });
        configSearch.addEventListener('keydown', function(ev) {
            if (ev.key === 'Enter') {
                ev.preventDefault();
                configBody.focus({ preventScroll: true });   // the rows found stay; a soft keyboard drops
            }
        });
        /* Keys as in the terminal's menu, except in a field being typed in: j and k
           move, h and l turn the row focused, / finds the search field, q goes back,
           and Ctrl-G, F10 and F1 go to the run, which closes the menu or opens help. */
        config.addEventListener('keydown', function(ev) {
            const typing = ev.target.matches('input, select, textarea');
            const plain = !ev.ctrlKey && !ev.altKey && !ev.metaKey;
            if (ev.key === 'F10' || ev.key === 'F1' || (ev.ctrlKey && !ev.shiftKey && ev.key.toLowerCase() === 'g')) {
                page.send({ type: 'key', key: ev.key, mods: TerminalPage.mods(ev), code: ev.code, repeat: ev.repeat });
                ev.preventDefault();
                return;
            }
            if (typing || !plain) return;
            const row = rowOf(ev.target);
            switch (ev.key) {
            case 'q':
                back();
                break;
            case 'j':
                moveFocus(1);
                break;
            case 'k':
                moveFocus(-1);
                break;
            case 'h':
                if (row) row.turn(-1);
                break;
            case 'l':
                if (row) row.turn(1);
                break;
            case '/':
                configSearch.focus();
                break;
            case 'Enter':
            case ' ':
                if (row && ev.target.classList.contains('row')) row.act();
                else return;
                break;
            default:
                return;
            }
            ev.preventDefault();
        });
    }

    // moveFocus takes the focus to the next or previous focusable row
    function moveFocus(direction) {
        const rows = [...configBody.querySelectorAll('.row[tabindex="0"]')];
        if (!rows.length) return;
        const at = rows.indexOf(document.activeElement.closest && document.activeElement.closest('.row'));
        const next = at < 0 ? (direction > 0 ? 0 : rows.length - 1) : Math.min(rows.length - 1, Math.max(0, at + direction));
        rows[next].focus();
    }

    // === Entry Point ===
    /* The page opens on TerminalPage's screen, the program held for the start screen's
       press: what the program posts of its own, the mixer's sound and the menu, and
       the documents it keeps come back here. */
    document.addEventListener('DOMContentLoaded', function() {
        const loading = document.getElementById('loading');
        let argv;
        wireMenu();
        try {
            // Before the download, so a refused argument vector is not paid for with one
            argv = launchArguments();
        } catch (err) {
            console.error('vif: WASM load failed', err);
            showMessage(FAILED.load(err.message), true);
            return;
        }
        page = TerminalPage.open({
            container: document.getElementById('terminal'),
            canvas: document.getElementById('screen'),
            input: input,
            strip: document.getElementById('strip'),
            wasm: WASM_PATH,
            worker: WORKER_PATH,
            exec: EXEC_PATH,
            argv: argv,
            kept: claimPlaces().then(function() { return { settings: keptSettings(), places: keptPlaces() }; }),
            features: ['menu', 'audio'],
            hold: true,
            cell: CELL,
            on: {
                progress: function(received, total) {
                    loading.textContent = 'Loading vif… ' + fmtProgress(received, total);
                },
                compiling: function() { loading.textContent = 'Compiling…'; },
                ready: function() {   // compiled and held for the start screen's tap
                    ready = true;
                    if (tapped) run();
                    else {
                        loading.textContent = PROMPT;
                        loading.classList.add('ask');
                    }
                },
                frame: function() {
                    loading.classList.add('hidden');   // compiled and drawing
                    if (!playing) {
                        playing = true;
                        if (navigator.userActivation && navigator.userActivation.isActive) arm();
                    }
                    if (!start.hidden && !away) start.hidden = true;
                },
                message: function(msg) {
                    if (msg.type === 'audio') onAudio(msg.data);
                    else if (msg.type === 'menu') applyMenu(msg.json);
                },
                keep: function(name, text) {
                    if (name === 'settings') keepSettings(text);
                    else if (name === 'places') keepPlaces(text);
                },
                gesture: arm,
                keyboard: keyboardShown,
                notice: notice,
                exit: function(code, reason) {
                    showExit(code, reason);
                    closeAudio();
                },
                failed: function(reason, stage) {
                    showMessage(FAILED[stage](reason), true);
                    if (stage === 'load') closeAudio();
                },
            },
        });
        wireStart();
        if (touch) wireStrip();
    });
})();
