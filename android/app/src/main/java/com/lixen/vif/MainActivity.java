package com.lixen.vif;

import android.app.Activity;
import android.content.pm.PackageManager;
import android.graphics.Insets;
import android.os.Bundle;
import android.util.Log;
import android.view.KeyEvent;
import android.view.WindowInsets;
import android.widget.FrameLayout;
import android.window.OnBackInvokedDispatcher;

import com.lixen.vif.game.Game;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;

/**
 * Hosts the game's view, copies the APK's wad to the files directory where the game
 * reads it as its system root, and passes the activity's lifecycle to the run.
 * Start with the extra "raster" set to paint with Go's software raster instead.
 */
public final class MainActivity extends Activity {
    static final String TAG = "vif";

    private Speaker speaker;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        getWindow().setDecorFitsSystemWindows(false);
        try {
            speaker = new Speaker();
        } catch (UnsupportedOperationException e) {
            Log.w(TAG, "no speakers; the game runs silent", e);
        }
        GameView view = new GameView(this, getFilesDir().getPath(), copyWad().getPath(),
                getIntent().getBooleanExtra("raster", false), speaker);
        FrameLayout root = new FrameLayout(this);
        root.addView(view);
        root.setOnApplyWindowInsetsListener((v, insets) -> {
            Insets bars = insets.getInsets(WindowInsets.Type.systemBars() | WindowInsets.Type.displayCutout());
            v.setPadding(bars.left, bars.top, bars.right, bars.bottom);
            // The keyboard covers the view rather than shrinking it: a resize crops a
            // crop_on_resize map, and what it cropped is gone when the keyboard is
            view.keyboardCovers(Math.max(0, insets.getInsets(WindowInsets.Type.ime()).bottom - bars.bottom));
            return WindowInsets.CONSUMED;
        });
        setContentView(root);
        // Back is the game's Escape, which leaves a mode; the run is quit with :q
        if (android.os.Build.VERSION.SDK_INT >= 33) {
            getOnBackInvokedDispatcher().registerOnBackInvokedCallback(
                    OnBackInvokedDispatcher.PRIORITY_DEFAULT, () -> Game.key(KeyEvent.KEYCODE_BACK, 0, 0));
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
        if (speaker != null) {
            speaker.resume();
        }
        Game.resume();
    }

    @Override
    protected void onPause() {
        Game.pause();
        if (speaker != null) {
            speaker.pause();
        }
        super.onPause();
    }

    @Override
    protected void onDestroy() {
        if (isFinishing()) {
            Game.quit();
            if (speaker != null) {
                speaker.close();
            }
        }
        super.onDestroy();
    }

    /** The run ended: by the player's :q, or by an error worth the log. */
    void exited(String err) {
        if (!err.isEmpty()) {
            Log.e(TAG, "game ended: " + err);
        }
        finish();
    }

    /** Copies the APK's wad once per install; the stamp is the install's update time. */
    private File copyWad() {
        File dir = new File(getFilesDir(), "wad");
        File stamp = new File(getFilesDir(), "wad.stamp");
        try {
            String installed = String.valueOf(
                    getPackageManager().getPackageInfo(getPackageName(), 0).lastUpdateTime);
            if (stamp.exists() && installed.equals(
                    new String(Files.readAllBytes(stamp.toPath()), StandardCharsets.UTF_8))) {
                return dir;
            }
            delete(dir);
            copy("wad", dir);
            Files.write(stamp.toPath(), installed.getBytes(StandardCharsets.UTF_8));
        } catch (IOException | PackageManager.NameNotFoundException e) {
            Log.e(TAG, "wad not copied; the game runs on its embedded files", e);
        }
        return dir;
    }

    private void copy(String asset, File to) throws IOException {
        String[] children = getAssets().list(asset);
        if (children == null || children.length == 0) {
            try (InputStream in = getAssets().open(asset); OutputStream out = new FileOutputStream(to)) {
                byte[] buf = new byte[8192];
                for (int n; (n = in.read(buf)) > 0; ) {
                    out.write(buf, 0, n);
                }
            }
            return;
        }
        if (!to.mkdirs() && !to.isDirectory()) {
            throw new IOException("cannot create " + to);
        }
        for (String child : children) {
            copy(asset + "/" + child, new File(to, child));
        }
    }

    private static void delete(File f) {
        File[] children = f.listFiles();
        if (children != null) {
            for (File child : children) {
                delete(child);
            }
        }
        f.delete();
    }
}
