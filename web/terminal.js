(function() {
    'use strict';

    const WASM_PATH = 'vi-fighter.wasm';

    let term;
    let fitAddon;
    let webglAddon;

    // === Write Batching (defensive, Go already batches) ===
    let pendingWrites = [];
    let writeScheduled = false;

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
        const text = new TextDecoder().decode(combined);
        term.write(text);
    }

    // Go → JS: Write output to terminal
    window.goTerminalWrite = function(data) {
        if (!term) return;

        const bytes = new Uint8Array(data.length);
        for (let i = 0; i < data.length; i++) {
            bytes[i] = data[i];
        }
        pendingWrites.push(bytes);

        if (!writeScheduled) {
            writeScheduled = true;
            queueMicrotask(flushWrites);
        }
    };

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

        // Expose for Go size query
        window.xterm = term;

        return term;
    }

    // === WASM Loading ===
    async function loadWasm() {
        if (!WebAssembly) {
            showError('WebAssembly not supported');
            return;
        }

        const go = new Go();

        try {
            const result = await WebAssembly.instantiateStreaming(
                fetch(WASM_PATH),
                go.importObject
            );

            document.getElementById('loading').classList.add('hidden');

            // Run Go main() - this blocks until Go yields
            go.run(result.instance);

            wireHandlers();

        } catch (err) {
            showError('Failed to load WASM: ' + err.message);
            console.error(err);
        }
    }

    // === Event Wiring ===
    function wireHandlers() {
        // Input: xterm → Go
        term.onData(function(data) {
            if (typeof window.goTerminalInput === 'function') {
                const encoder = new TextEncoder();
                const bytes = encoder.encode(data);
                const arr = new Uint8Array(bytes);
                window.goTerminalInput(arr);
            }
        });

        term.onBinary(function(data) {
            if (typeof window.goTerminalInput === 'function') {
                const arr = new Uint8Array(data.length);
                for (let i = 0; i < data.length; i++) {
                    arr[i] = data.charCodeAt(i);
                }
                window.goTerminalInput(arr);
            }
        });

        // Resize handling
        function handleResize() {
            if (!fitAddon) return;
            fitAddon.fit();

            if (typeof window.goTerminalResize === 'function') {
                window.goTerminalResize(term.cols, term.rows);
            }
        }

        // Prevent context menu on terminal to ensure right-click release events reach xterm
        const container = document.getElementById('terminal');
        container.addEventListener('contextmenu', function(e) {
            e.preventDefault();
            return false;
        });

        window.addEventListener('resize', handleResize);
        setTimeout(handleResize, 100);

        // Focus terminal for keyboard capture
        // Works for full-page; iframe may require user interaction first
        term.focus();

        // Re-focus on click (handles iframe activation)
        container.addEventListener('click', function() {
            term.focus();
        });
    }

    function showError(msg) {
        const el = document.getElementById('loading');
        el.textContent = msg;
        el.style.color = '#f44';
    }

    // === Entry Point ===
    document.addEventListener('DOMContentLoaded', function() {
        initTerminal();
        loadWasm();
    });
})();