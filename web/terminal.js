(function() {
    'use strict';

    /* The files beside the page, by the names web/ gives them. A site that
       publishes them under other names, as content-hashed ones, says so on this
       script's element: data-wasm, data-worker and data-exec (wasm_exec.js). */
    const files = (document.currentScript && document.currentScript.dataset) || {};
    const WASM_PATH = files.wasm || 'vif.wasm';
    const WORKER_PATH = files.worker || 'worker.js';
    const EXEC_PATH = files.exec || 'wasm_exec.js';

    /* Touch, decided once at load. Any touch screen keeps the system keyboard
       down on taps; a touch-first one (phone, tablet) also gets the key strip. A
       phone draws at 10px, about what 14px is on a laptop at their viewing distances. */
    const anyTouch = matchMedia('(any-pointer: coarse)').matches;
    const touch = matchMedia('(pointer: coarse)').matches;
    const FONT = touch && Math.min(screen.width, screen.height) < 600 ? 10 : 14;

    let term;
    let fitAddon;
    let webglAddon;
    let program;   // the Worker running Go; see worker.js

    // === Write Batching (defensive, Go already batches) ===
    let pendingWrites = [];
    let writeScheduled = false;
    // Streaming, and one instance: a UTF-8 sequence split across two flushes
    // must not decode as replacement characters.
    const decoder = new TextDecoder('utf-8');

    function flushWrites() {
        writeScheduled = false;
        if (pendingWrites.length === 0) return;

        // Concatenate all pending
        const total = pendingWrites.reduce((sum, arr) => sum + arr.length, 0);
        const combined = new Uint8Array(total);
        let offset = 0;
        for (const arr of pendingWrites) {
            combined.set(arr, offset);
            offset += arr.length;
        }
        pendingWrites = [];

        // Single write to xterm
        term.write(decoder.decode(combined, { stream: true }));
    }

    // Go → JS: output to the terminal
    function terminalWrite(data) {
        if (!term) return;
        pendingWrites.push(data);
        if (!writeScheduled) {
            writeScheduled = true;
            queueMicrotask(flushWrites);
        }
    }

    // === Terminal Initialization ===
    function initTerminal() {
        term = new Terminal({
            cursorBlink: false,
            cursorStyle: 'block',
            allowProposedApi: true,
            scrollback: 0,
            fontFamily: '"JetBrains Mono", "Fira Code", "SF Mono", Menlo, monospace',
            fontSize: FONT,
            lineHeight: 1.0,
            letterSpacing: 0,
            theme: {
                background: '#000000',
                foreground: '#ffffff',
                cursor: '#ffffff',
                cursorAccent: '#000000'
            }
        });

        fitAddon = new FitAddon.FitAddon();
        term.loadAddon(fitAddon);

        // WebGL addon (optional, graceful fallback)
        try {
            webglAddon = new WebglAddon.WebglAddon();
            webglAddon.onContextLoss(() => {
                webglAddon.dispose();
                webglAddon = null;
            });
            term.loadAddon(webglAddon);
        } catch (e) {
            console.warn('WebGL addon unavailable, using canvas renderer');
        }

        const container = document.getElementById('terminal');
        if (touch) document.getElementById('strip').hidden = false;   // before the first fit
        term.open(container);
        // xterm focuses this field on every tap; so marked, the focus raises no
        // keyboard, while hardware keys still type through it.
        if (anyTouch) term.textarea.setAttribute('inputmode', 'none');
        fitAddon.fit();
        fitted();
        if (touch) wireTouch();

        return term;
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

    function showExit(code, reason) {
        if (code === 0) {
            showMessage('vif has exited. Reload to start again.', false);
            return;
        }
        reason = (reason || 'exit code ' + code).replace(/\.$/, '');
        showMessage('vif stopped: ' + reason + '. Reload to retry.', true);
    }

    // === WASM Loading ===
    const MB = 1048576; // 1024 * 1024 bytes in 1 Megabyte
    const fmtProgress = (received, total) => total
        ? (received / MB).toFixed(1) + ' / ' + (total / MB).toFixed(1) + ' MB'
        : (received / MB).toFixed(1) + ' MB';

    async function loadWasm() {
        // typeof, not a bare identifier: `!WebAssembly` throws a ReferenceError
        // on a browser that lacks it, which is the case being tested for.
        if (typeof WebAssembly === 'undefined') {
            showMessage('WebAssembly is not supported by this browser.', true);
            return;
        }

        const loading = document.getElementById('loading');
        let argv, bytes;

        try {
            // Before the fetch, so a refused argument vector is not paid for with
            // a download first.
            argv = launchArguments();

            const response = await fetch(WASM_PATH);
            // Missing before: instantiateStreaming was fed the 404 page and
            // reported a magic-word error instead of the HTTP status.
            if (!response.ok) throw new Error('HTTP ' + response.status);

            /* Buffered rather than instantiateStreaming, to report progress: a
               tee()'d body cannot be handed to instantiateStreaming because both
               branches share one source. Cost is that compilation starts after
               the download instead of during it. */
            if (response.body) {
                const reader = response.body.getReader();
                const chunks = [];
                let received = 0;
                let total = Number(response.headers.get('content-length')) || 0;
                for (;;) {
                    const { done, value } = await reader.read();
                    if (done) break;
                    chunks.push(value);
                    received += value.length;
                    if (total && received > total) total = 0;   // compressed length; drop it
                    loading.textContent = 'Loading vif… ' + fmtProgress(received, total);
                }
                bytes = new Uint8Array(received);
                let offset = 0;
                for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
            } else {
                bytes = new Uint8Array(await response.arrayBuffer());
            }

            loading.textContent = 'Compiling…';
        } catch (err) {
            console.error('vif: WASM load failed', err);
            showMessage('Failed to load vif (' + err.message + '). Reload to retry.', true);
            return;
        }

        program = new Worker(WORKER_PATH);
        program.onerror = function(e) {
            if (!program) return;   // a timer Go left behind throws after exit; the exit stands
            showMessage('Failed to start vif (' + e.message + '). Reload to retry.', true);
        };
        program.onmessage = function(e) {
            const msg = e.data;
            switch (msg.type) {
            case 'write':
                loading.classList.add('hidden');   // compiled and drawing
                terminalWrite(msg.data);
                break;
            case 'exit':
                showExit(msg.code, msg.reason);
                stop();
                break;
            case 'failed':
                showMessage('Failed to load vif (' + msg.reason + '). Reload to retry.', true);
                stop();
                break;
            }
        };
        // Resolved here: the worker would resolve a relative name against its own URL.
        program.postMessage({ type: 'start', exec: new URL(EXEC_PATH, document.baseURI).href,
            bytes: bytes.buffer, argv: argv, cols: term.cols, rows: term.rows }, [bytes.buffer]);
        document.addEventListener('visibilitychange', function() {
            if (program) program.postMessage({ type: 'visibility', hidden: document.hidden });
        });
        wireHandlers();
    }

    /* An ended program takes no more messages: a resize or a key posted to it
       would fail there, and its error would replace the reason it ended. */
    function stop() {
        if (!program) return;
        program.terminate();
        program = null;
        if (touch) term.blur();   // nothing left to type into, and a keyboard would cover the message
    }

    function sendInput(arr) {
        if (program) program.postMessage({ type: 'input', data: arr }, [arr.buffer]);
    }

    // === Event Wiring ===
    function wireHandlers() {
        // Input: xterm → Go
        term.onData(function(data) {
            sendInput(new TextEncoder().encode(data));
        });

        term.onBinary(function(data) {
            const arr = new Uint8Array(data.length);
            for (let i = 0; i < data.length; i++) {
                arr[i] = data.charCodeAt(i);
            }
            sendInput(arr);
        });

        /* Sizing. Mirrors FitAddon's own arithmetic: parent computed height minus
           .xterm's padding. fit() floors rows against the cell height it knew at
           call time, which can be stale after a zoom step, so verify and drop a
           row if the grid paints taller than the box. */
        function availHeight() {
            const parentCS = getComputedStyle(term.element.parentElement);
            const selfCS = getComputedStyle(term.element);
            return parseFloat(parentCS.height)
                 - parseFloat(selfCS.paddingTop) - parseFloat(selfCS.paddingBottom);
        }

        function snapRows() {
            const scr = term.element && term.element.querySelector('.xterm-screen');
            if (!scr || !term.rows) return;
            const cell = scr.getBoundingClientRect().height / term.rows;
            if (!cell) return;
            const max = Math.max(1, Math.floor(availHeight() / cell));
            if (term.rows > max) term.resize(term.cols, max);
        }

        function fitNow(passes) {
            if (!fitAddon) return;
            if (term.options.fontSize !== FONT) term.options.fontSize = FONT;   // never fit at the keyboard's size
            fitAddon.fit();
            snapRows();
            if (passes > 1) {
                requestAnimationFrame(function() { fitNow(passes - 1); });
                return;
            }
            fitted();
            if (program) program.postMessage({ type: 'resize', cols: term.cols, rows: term.rows });
            if (touch) layoutKeyboard();
        }

        let fitTimer = 0;
        function handleResize() {
            // Where a soft keyboard shrinks the page itself, the grid must not follow it.
            if ((keyboard || keyboardUp) && document.documentElement.clientWidth === fitW) {
                refitPending = true;
                return;
            }
            clearTimeout(fitTimer);
            fitTimer = setTimeout(function() { fitNow(2); }, 50);
        }

        // A resolution query matches one ratio only, so re-arm on every change.
        let dprQuery = null;
        function onDprChange() { watchDpr(); handleResize(); }
        function watchDpr() {
            if (dprQuery) dprQuery.removeEventListener('change', onDprChange);
            dprQuery = window.matchMedia('(resolution: ' + window.devicePixelRatio + 'dppx)');
            dprQuery.addEventListener('change', onDprChange);
        }

        // Prevent context menu on terminal to ensure right-click release events reach xterm
        const container = document.getElementById('terminal');
        container.addEventListener('contextmenu', function(e) {
            e.preventDefault();
            return false;
        });

        window.addEventListener('resize', handleResize);
        watchDpr();
        setTimeout(function() { fitNow(2); }, 100);

        // Focus terminal for keyboard capture
        // Works for full-page; iframe may require user interaction first
        term.focus();

        // Re-focus on click (handles iframe activation)
        container.addEventListener('click', function() {
            term.focus();
        });
    }

    // === Touch: key strip and system keyboard ===
    const STRIP_KEYS = { tab: '\t', esc: '\x1b' };
    const KEYBOARD_MIN = 100;   // px of screen a soft keyboard takes at least; less is a toolbar
    const FONT_MIN = 6;         // smallest the grid is drawn to fit above the keyboard
    let keyboard = false;       // the player asked for the system keyboard
    let keyboardSeen = false;   // and it came up, so its going away is the system dismissing it
    let keyboardUp = false;     // a keyboard the player asked for covers the page, or is still leaving
    let refitPending = false;   // a resize put off while that keyboard was asked for or up
    let fitW = 0, fitH = 0;     // page size the grid was last fitted to
    let layoutKeyboard = function() {};

    function fitted() {
        fitW = document.documentElement.clientWidth;
        fitH = document.documentElement.clientHeight;
    }

    function wireTouch() {
        const strip = document.getElementById('strip');
        const kbdButton = document.getElementById('kbd');
        const escButton = document.getElementById('esc');
        const fullButton = document.getElementById('full');
        const container = document.getElementById('terminal');
        const gridEl = term.element.querySelector('.xterm-screen');
        const vv = window.visualViewport;

        // A strip tap must leave focus, and with it the keyboard, where it is.
        strip.addEventListener('pointerdown', function(e) { e.preventDefault(); });
        strip.addEventListener('click', function(e) {
            const b = e.target.closest('button');
            if (!b) return;
            if (b.dataset.key) term.input(STRIP_KEYS[b.dataset.key]);   // the path typed keys take
            else if (b === kbdButton) setKeyboard(!keyboard, true);
            else if (b === fullButton) toggleFullscreen();
        });

        function setKeyboard(on, refocus) {
            keyboard = on;
            keyboardSeen = false;
            term.textarea.setAttribute('inputmode', on ? 'text' : 'none');
            // A focused field keeps the keyboard it has: refocusing inside the tap is what
            // raises or drops it, and the only way iOS raises one at all.
            if (refocus) {
                term.blur();
                term.focus();
            }
            kbdButton.setAttribute('aria-pressed', on);
            escButton.hidden = !on;
            layoutKeyboard();
        }

        /* While a keyboard covers the page the grid keeps its size, which is the solo
           map's, and is drawn at a smaller font between the strip and the keyboard;
           below FONT_MIN it keeps the bottom rows, where commands are typed, in view. */
        function drawAt(size) {
            if (term.options.fontSize !== size) term.options.fontSize = size;
            return gridEl.getBoundingClientRect().height;
        }

        let fullHeight = 0;   // the grid's height at FONT
        let lastFit = null;   // the size found for a height, kept while the keyboard holds still
        layoutKeyboard = function() {
            const up = !!vv && fitH - vv.height * vv.scale > KEYBOARD_MIN;
            if (keyboard && up) keyboardSeen = true;
            else if (keyboard && keyboardSeen) return setKeyboard(false, false);   // dismissed by the system
            keyboardUp = up && (keyboard || keyboardUp);

            if (term.options.fontSize === FONT) {
                fullHeight = gridEl.getBoundingClientRect().height;
                lastFit = null;
            }
            let shift = 0;
            if (up) {
                const cs = getComputedStyle(term.element);
                const avail = vv.height - strip.offsetHeight
                    - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom);
                let size = lastFit && lastFit.avail === avail ? lastFit.size
                    : Math.max(FONT_MIN, Math.min(FONT, Math.floor(2 * FONT * avail / fullHeight) / 2));
                let h;
                while ((h = drawAt(size)) > avail && size > FONT_MIN) size -= 0.5;
                lastFit = { avail: avail, size: size };
                shift = Math.max(0, h - avail);
            } else {
                drawAt(FONT);
                // Only once the page is whole again does a put-off refit see its real size.
                if (!keyboard && refitPending) {
                    refitPending = false;
                    window.dispatchEvent(new Event('resize'));
                }
            }
            // The page follows the visible area if the browser pans it to the keyboard.
            document.body.style.transform = up && vv.offsetTop ? 'translateY(' + vv.offsetTop + 'px)' : '';
            container.style.transform = shift ? 'translateY(' + -shift + 'px)' : '';
        };

        if (vv) {
            let queued = false;
            const schedule = function() {
                if (queued) return;
                queued = true;
                requestAnimationFrame(function() { queued = false; layoutKeyboard(); });
            };
            vv.addEventListener('resize', schedule);
            vv.addEventListener('scroll', schedule);
        }

        // Fullscreen refits the grid as a rotation does; iPhone Safari has no API for it.
        if (document.fullscreenEnabled) {
            fullButton.hidden = false;
            document.addEventListener('fullscreenchange', function() {
                fullButton.setAttribute('aria-pressed', document.fullscreenElement !== null);
            });
        }
        function toggleFullscreen() {
            const done = document.fullscreenElement
                ? document.exitFullscreen()
                : document.documentElement.requestFullscreen({ navigationUI: 'hide' });
            done.catch(function() {});
        }
    }

    function showMessage(msg, isError) {
        const el = document.getElementById('loading');
        el.textContent = msg;
        el.classList.remove('hidden');
        el.classList.toggle('error', isError);
    }

    // === Entry Point ===
    document.addEventListener('DOMContentLoaded', function() {
        initTerminal();
        loadWasm();
    });
})();
