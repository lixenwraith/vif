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
import android.view.SurfaceHolder;
import android.view.SurfaceView;
import android.view.inputmethod.BaseInputConnection;
import android.view.inputmethod.EditorInfo;
import android.view.inputmethod.InputConnection;
import android.view.inputmethod.InputMethodManager;

import com.lixen.vif.game.Game;

import java.util.Arrays;

/**
 * The game's screen: a render thread takes the newest frame the run flushed at each
 * vsync, hands it to the drawer and posts the drawer's picture. Taps and keys go to
 * the run; a two-finger tap raises or drops the system keyboard.
 */
final class GameView extends SurfaceView implements SurfaceHolder.Callback, Choreographer.FrameCallback {
    /** A cell's height in dp: the atlas's own 24 pixels are 9 dp on a 420 dpi screen. */
    private static final float CELL_DP = 18;

    private final MainActivity activity;
    private final String files, wad;
    private final boolean raster;
    private final Speaker speaker;
    private final int scale;
    private final CellDrawer drawer;
    private final Stats stats = new Stats();

    private boolean started;
    private boolean keyboard;
    private HandlerThread thread;
    private Handler handler;
    private boolean looping; // a frame callback is posted on the render thread

    GameView(MainActivity activity, String files, String wad, boolean raster, Speaker speaker) {
        super(activity);
        this.activity = activity;
        this.files = files;
        this.wad = wad;
        this.raster = raster;
        this.speaker = speaker;
        byte[] atlas;
        try {
            atlas = Game.atlas();
        } catch (Exception e) {
            throw new IllegalStateException("font atlas", e);
        }
        float density = activity.getResources().getDisplayMetrics().density;
        scale = Math.max(1, Math.round(density * CELL_DP / CellDrawer.words(atlas, 0).get(1)));
        drawer = raster ? new CellDrawer.FromRaster(atlas, scale) : new CellDrawer.OnCanvas(atlas, scale);
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
                Game.start(files, wad, width, height, scale, raster, speaker,
                        err -> activity.runOnUiThread(() -> activity.exited(err)));
            } catch (Exception e) {
                Log.e(MainActivity.TAG, "game not started", e);
                activity.finish();
                return;
            }
            started = true;
            requestFocus();
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
        } finally {
            holder.unlockCanvasAndPost(c);
        }
    }

    @Override
    public boolean onTouchEvent(MotionEvent e) {
        int x = (int) e.getX(), y = (int) e.getY();
        switch (e.getActionMasked()) {
            case MotionEvent.ACTION_DOWN:
                Game.tap(x, y, true);
                break;
            case MotionEvent.ACTION_UP:
            case MotionEvent.ACTION_CANCEL:
                Game.tap(x, y, false);
                break;
            case MotionEvent.ACTION_POINTER_DOWN:
                if (e.getPointerCount() == 2) {
                    Game.tap(x, y, false);
                    toggleKeyboard();
                }
                break;
        }
        return true;
    }

    private void toggleKeyboard() {
        InputMethodManager im = getContext().getSystemService(InputMethodManager.class);
        keyboard = !keyboard;
        if (keyboard) {
            requestFocus();
            im.showSoftInput(this, 0);
        } else {
            im.hideSoftInputFromWindow(getWindowToken(), 0);
        }
    }

    @Override
    public boolean onKeyDown(int code, KeyEvent e) {
        int typed = e.getUnicodeChar(e.getMetaState() & ~(KeyEvent.META_CTRL_MASK | KeyEvent.META_ALT_MASK));
        if ((typed & KeyCharacterMap.COMBINING_ACCENT) != 0) {
            typed = 0;
        }
        return Game.key(code, typed, e.getMetaState()) || super.onKeyDown(code, e);
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
