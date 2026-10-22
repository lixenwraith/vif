package com.lixen.vif;

import android.graphics.Canvas;
import android.os.Handler;
import android.os.HandlerThread;
import android.os.Process;
import android.text.InputType;
import android.util.Log;
import android.view.Choreographer;
import android.view.KeyCharacterMap;
import android.view.KeyEvent;
import android.view.MotionEvent;
import android.view.ScaleGestureDetector;
import android.view.SurfaceHolder;
import android.view.SurfaceView;
import android.view.ViewConfiguration;
import android.view.WindowInsets;
import android.view.WindowInsetsController;
import android.view.inputmethod.BaseInputConnection;
import android.view.inputmethod.EditorInfo;
import android.view.inputmethod.InputConnection;

import com.lixen.vif.game.Game;

import java.util.Arrays;

/**
 * The game's screen: a render thread takes the newest frame the run flushed at each
 * vsync, hands it to the drawer and posts the drawer's picture. Taps, drags and keys
 * go to the run; two fingers tapped together twice raise or drop the system
 * keyboard, which covers the picture rather than resizing it; the left half of the
 * status bar's bottom row, its mode and input, is then drawn again above the
 * keyboard, twice its size, so what is typed can be read.
 */
final class GameView extends SurfaceView implements SurfaceHolder.Callback, Choreographer.FrameCallback {
    // Two fingers are a tap when the second lands this soon after the first, neither
    // moves, and both lift this soon after; a finger joining a held one is play. Two
    // such taps this close together toggle the keyboard: one still happens in play
    private static final long PAIR_TOGETHER_MS = 150, PAIR_TAP_MS = 300, PAIR_DOUBLE_MS = 500;

    private final MainActivity activity;
    private final String files, wad;
    private final boolean raster;
    private final Speaker speaker;
    private final CellDrawer drawer;
    private final Stats stats = new Stats();
    private final int slop;
    private final ScaleGestureDetector scaler; // a pinch zooms the cells
    private float pinch; // the scale of the pinch in progress, which the game zooms by at its end
    private boolean scaling; // a pinch is in progress: its fingers are no taps

    private boolean started;
    private HandlerThread thread;
    private Handler handler;
    private boolean looping; // a frame callback is posted on the render thread
    private volatile int covered; // the surface's bottom pixels under the keyboard

    private long downAt, pairAt; // when the first finger landed, and the second
    private long pairTapAt; // when the last pair tap ended, 0 once it was used or stale
    private boolean pairDown, pairTap; // two fingers rest together; they tapped
    private final float[] pairX = new float[2], pairY = new float[2];

    GameView(MainActivity activity, String files, String wad, boolean raster, Speaker speaker) {
        super(activity);
        this.activity = activity;
        this.files = files;
        this.wad = wad;
        this.raster = raster;
        this.speaker = speaker;
        CellDrawer d;
        try {
            byte[] atlas = Game.atlas();
            d = raster ? new CellDrawer.FromRaster(atlas) : new CellDrawer.OnCanvas(atlas);
        } catch (Exception e) {
            throw new IllegalStateException("font atlas", e);
        }
        drawer = d;
        slop = ViewConfiguration.get(activity).getScaledTouchSlop();
        scaler = new ScaleGestureDetector(activity, new ScaleGestureDetector.SimpleOnScaleGestureListener() {
            @Override
            public boolean onScaleBegin(ScaleGestureDetector d) {
                scaling = true;
                pinch = 1;
                pairDown = pairTap = false;
                Game.tap((int) d.getFocusX(), (int) d.getFocusY(), false);
                return true;
            }

            @Override
            public boolean onScale(ScaleGestureDetector d) {
                pinch *= d.getScaleFactor();
                return true;
            }

            @Override
            public void onScaleEnd(ScaleGestureDetector d) {
                Game.zoom(pinch);
            }
        });
        scaler.setQuickScaleEnabled(false); // a double tap is special fire, not a zoom handle
        setFocusable(true);
        setFocusableInTouchMode(true);
        getHolder().addCallback(this);
    }

    @Override
    public void surfaceCreated(SurfaceHolder holder) {
        thread = new HandlerThread("vif-render", Process.THREAD_PRIORITY_DISPLAY);
        thread.start();
        handler = new Handler(thread.getLooper());
    }

    @Override
    public void surfaceChanged(SurfaceHolder holder, int format, int width, int height) {
        if (!started) {
            try {
                Game.start(files, wad, width, height, getResources().getDisplayMetrics().density, raster,
                        activity.waiting(), speaker, err -> activity.runOnUiThread(() -> activity.exited(err)));
            } catch (Exception e) {
                Log.e(MainActivity.TAG, "game not started", e);
                activity.finish();
                return;
            }
            started = true;
        } else {
            Game.resize(width, height);
        }
        handler.post(() -> {
            present();
            if (!looping) {
                looping = true;
                Choreographer.getInstance().postFrameCallback(this);
            }
        });
    }

    @Override
    public void surfaceDestroyed(SurfaceHolder holder) {
        // Nothing may draw on the surface once this returns
        thread.quitSafely();
        try {
            thread.join();
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
        looping = false; // the next surface's thread posts its own callback
    }

    @Override
    public void doFrame(long vsyncNanos) {
        Choreographer.getInstance().postFrameCallback(this);
        long start = System.nanoTime();
        byte[] frame = Game.frame();
        if (frame != null && drawer.apply(frame)) {
            present();
            stats.add(System.nanoTime() - start);
        }
    }

    // A hardware canvas: the GPU composites the picture, where a software one costs a
    // copy of every pixel each frame
    private void present() {
        SurfaceHolder holder = getHolder();
        Canvas c = holder.lockHardwareCanvas();
        if (c == null) {
            return;
        }
        try {
            drawer.draw(c);
            int under = covered;
            if (under > 0) {
                drawer.drawStrip(c, under);
            }
        } finally {
            holder.unlockCanvasAndPost(c);
        }
    }

    /** The keyboard covers the bottom px of the surface; the picture keeps its grid. */
    void keyboardCovers(int px) {
        if (covered == px) {
            return;
        }
        if ((covered > 0) != (px > 0)) {
            Game.keyboard(px > 0);
        }
        covered = px;
        if (handler != null) {
            handler.post(this::present);
        }
    }

    @Override
    public boolean onTouchEvent(MotionEvent e) {
        int x = (int) e.getX(), y = (int) e.getY();
        if (covered > 0 && y >= getHeight() - covered - drawer.stripHeight(getHeight())) {
            return true; // the strip is a readout, not the map under it
        }
        scaler.onTouchEvent(e);
        if (scaling) {
            if (e.getActionMasked() == MotionEvent.ACTION_UP || e.getActionMasked() == MotionEvent.ACTION_CANCEL) {
                scaling = false;
            }
            return true;
        }
        switch (e.getActionMasked()) {
            case MotionEvent.ACTION_DOWN:
                pairDown = pairTap = false;
                downAt = e.getEventTime();
                Game.tap(x, y, true);
                break;
            case MotionEvent.ACTION_POINTER_DOWN:
                // A second finger landing with the first starts a tap; one joining a
                // finger held or dragging, as thumbs do in landscape, is nothing
                if (e.getPointerCount() == 2 && e.getEventTime() - downAt <= PAIR_TOGETHER_MS) {
                    pairDown = true;
                    pairAt = e.getEventTime();
                    for (int i = 0; i < 2; i++) {
                        pairX[i] = e.getX(i);
                        pairY[i] = e.getY(i);
                    }
                    Game.tap(x, y, false);
                }
                break;
            case MotionEvent.ACTION_MOVE:
                if (!pairDown) {
                    Game.drag(x, y);
                    break;
                }
                for (int i = 0; i < Math.min(2, e.getPointerCount()); i++) {
                    if (Math.abs(e.getX(i) - pairX[i]) > slop || Math.abs(e.getY(i) - pairY[i]) > slop) {
                        pairDown = false;
                    }
                }
                break;
            case MotionEvent.ACTION_POINTER_UP:
                pairTap = pairDown && e.getPointerCount() == 2 && e.getEventTime() - pairAt <= PAIR_TAP_MS;
                pairDown = false;
                break;
            case MotionEvent.ACTION_UP:
                Game.tap(x, y, false);
                if (pairTap && e.getEventTime() - pairAt <= PAIR_TAP_MS) {
                    if (pairTapAt != 0 && e.getEventTime() - pairTapAt <= PAIR_DOUBLE_MS) {
                        pairTapAt = 0;
                        toggleKeyboard();
                    } else {
                        pairTapAt = e.getEventTime();
                    }
                }
                pairTap = false;
                break;
            case MotionEvent.ACTION_CANCEL:
                pairDown = pairTap = false;
                Game.tap(x, y, false);
                break;
        }
        return true;
    }

    // The window's own word on the keyboard, not a flag of ours: Back drops it
    // without telling the app, which left a flag a toggle behind
    private void toggleKeyboard() {
        WindowInsetsController c = getWindowInsetsController();
        WindowInsets insets = getRootWindowInsets();
        if (c == null) {
            return;
        }
        if (insets != null && insets.isVisible(WindowInsets.Type.ime())) {
            c.hide(WindowInsets.Type.ime());
        } else {
            requestFocus();
            c.show(WindowInsets.Type.ime());
        }
    }

    @Override
    public boolean onKeyDown(int code, KeyEvent e) {
        if (code == KeyEvent.KEYCODE_BACK && android.os.Build.VERSION.SDK_INT < 33) {
            activity.back();
            return true;
        }
        return Game.keyDown(code, typed(e), e.getMetaState(), e.getRepeatCount() > 0) || super.onKeyDown(code, e);
    }

    @Override
    public boolean onKeyUp(int code, KeyEvent e) {
        if (code == KeyEvent.KEYCODE_BACK && android.os.Build.VERSION.SDK_INT < 33) {
            return true;
        }
        return Game.keyUp(code, typed(e), e.getMetaState()) || super.onKeyUp(code, e);
    }

    /** The character a key types without Ctrl or Alt, 0 for none or a dead key. */
    private static int typed(KeyEvent e) {
        int typed = e.getUnicodeChar(e.getMetaState() & ~(KeyEvent.META_CTRL_MASK | KeyEvent.META_ALT_MASK));
        return (typed & KeyCharacterMap.COMBINING_ACCENT) != 0 ? 0 : typed;
    }

    // Keys held when the window loses focus send their ups elsewhere
    @Override
    public void onWindowFocusChanged(boolean focused) {
        super.onWindowFocusChanged(focused);
        if (!focused) {
            Game.focusLost();
        }
    }

    @Override
    public boolean onCheckIsTextEditor() {
        return true;
    }

    @Override
    public InputConnection onCreateInputConnection(EditorInfo out) {
        out.inputType = InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD
                | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS;
        out.imeOptions = EditorInfo.IME_FLAG_NO_EXTRACT_UI | EditorInfo.IME_FLAG_NO_FULLSCREEN
                | EditorInfo.IME_ACTION_NONE;
        return new BaseInputConnection(this, false) {
            @Override
            public boolean commitText(CharSequence text, int cursor) {
                Game.text(text.toString());
                return true;
            }

            @Override
            public boolean deleteSurroundingText(int before, int after) {
                for (int i = 0; i < before; i++) {
                    Game.key(KeyEvent.KEYCODE_DEL, 0, 0);
                }
                return true;
            }
        };
    }

    /** A frame's cost on the render thread, take, paint and post, logged every five seconds. */
    private static final class Stats {
        private final long[] nanos = new long[1024];
        private int n;
        private long since = System.nanoTime();

        void add(long t) {
            if (n < nanos.length) {
                nanos[n++] = t;
            }
            long now = System.nanoTime();
            if (now - since < 5_000_000_000L || n == 0) {
                return;
            }
            long[] s = Arrays.copyOf(nanos, n);
            Arrays.sort(s);
            long sum = 0;
            for (long v : s) {
                sum += v;
            }
            Log.i(MainActivity.TAG, String.format("frames %.1f/s, frame time mean %.2f ms, p95 %.2f ms, max %.2f ms",
                    n * 1e9 / (now - since), sum / 1e6 / n, s[n * 95 / 100] / 1e6, s[n - 1] / 1e6));
            n = 0;
            since = now;
        }
    }
}
