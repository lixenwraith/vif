(function() {
    'use strict';

    const WASM_PATH = 'vif.wasm';

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
            fontSize: 14,
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
        term.open(container);
        fitAddon.fit();

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

        program = new Worker('worker.js');
        program.onerror = function(e) {
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
                break;
            case 'failed':
                showMessage('Failed to load vif (' + msg.reason + '). Reload to retry.', true);
                break;
            }
        };
        program.postMessage({ type: 'start', bytes: bytes.buffer, argv: argv,
            cols: term.cols, rows: term.rows }, [bytes.buffer]);
        wireHandlers();
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
            fitAddon.fit();
            snapRows();
            if (passes > 1) {
                requestAnimationFrame(function() { fitNow(passes - 1); });
                return;
            }
            if (program) program.postMessage({ type: 'resize', cols: term.cols, rows: term.rows });
        }

        let fitTimer = 0;
        function handleResize() {
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
