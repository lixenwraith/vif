package com.lixen.vif;

import android.app.Dialog;
import android.content.Context;
import android.graphics.Color;
import android.graphics.Insets;
import android.graphics.Typeface;
import android.graphics.drawable.ColorDrawable;
import android.graphics.drawable.GradientDrawable;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowInsets;
import android.widget.FrameLayout;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

/**
 * The screen the app opens on, over the game, filling the window as Android's splash
 * screen before it does: the logo as large and where the splash showed it, "Tap to
 * start" under it on a button and a ? in the corner that opens the touch controls. The
 * run waits behind it, paused, until a tap anywhere else. A touch browser shows the
 * same screen (web/index.html); both wear lixen.com's dark theme in Roboto.
 */
final class StartScreen extends FrameLayout {
    // The touch controls as web/index.html words them; Back is each platform's own.
    // The status bar's come last under their own title, which says where the bar is
    private static final String[][] CONTROLS = {
            {"Tap", "Move the cursor there and fire the main weapon; hold to repeat"},
            {"Drag", "Drag the cursor with your finger"},
            {"Double tap", "Fire the special weapon as well"},
            {"Pinch", "Zoom in or out; the zoom is kept for next time"},
            {"Back", "Escape; twice quickly, leave with the game paused"},
    };
    private static final String BAR_TITLE = "Status bar";
    private static final String BAR = "The strip along the bottom edge of the screen: the mode, messages and what you type.";
    private static final String[][] BAR_CONTROLS = {
            {"Tap and hold", "Open the configuration menu: audio, bots, multiplayer and more"},
            {"Triple tap", "Show or hide the keyboard, to type commands such as :help"},
    };
    private static final String KEYBOARD = "With a keyboard, F1 or the command :help lists every key and command, and the command :q quits.";
    // The site's surface, hairline, muted text, the primary button's blue and the royal
    // blue it fills while pressed, the periwinkle a ring of that blue is labelled in and
    // its red, as web/terminal.css names them; its page is R.color.page, which the
    // window and the splash wear too. The menu (MenuDialog) wears the same.
    static final int SURFACE = 0xFF1A1B26, LINE = 0x26FFFFFF, MUTED = 0xFF94A3B8, BUTTON = 0xFF2563EB,
            HOVER = 0xFF5F57F5, ACCENT = 0xFF8EA2FF, DANGER = 0xFFF87171;
    // The buttons' fade in after the splash, which showed the logo alone while the app loaded
    private static final long FADE_MS = 150;
    static final Typeface MEDIUM = Typeface.create("sans-serif-medium", Typeface.NORMAL);

    private final int logoSide;
    private final TextView prompt;
    private final TextView help;

    StartScreen(Context context, Runnable start) {
        super(context);
        setBackgroundColor(context.getColor(R.color.page));
        setOnClickListener(v -> start.run());
        // The whole screen is the button, so a keyboard's focus would wash all of it
        // lighter than the bars around it; the ? keeps its own highlight
        setDefaultFocusHighlightEnabled(false);
        ImageView logo = new ImageView(context);
        logo.setImageResource(R.drawable.logo);
        logo.setContentDescription("lixen");
        logoSide = getResources().getDimensionPixelSize(R.dimen.logo);
        addView(logo, new LayoutParams(logoSide, logoSide, Gravity.CENTER));
        prompt = text("Tap to start", 18, Color.WHITE);
        prompt.setTypeface(MEDIUM);
        prompt.setPadding(dp(28), dp(12), dp(28), dp(12));
        GradientDrawable fill = new GradientDrawable();
        fill.setColor(BUTTON);
        fill.setCornerRadius(dp(8));
        prompt.setBackground(fill);
        addView(prompt, new LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT,
                Gravity.TOP | Gravity.CENTER_HORIZONTAL));
        help = text("?", 24, ACCENT);
        help.setTypeface(MEDIUM);
        help.setGravity(Gravity.CENTER);
        help.setContentDescription("Touch controls");
        GradientDrawable ring = new GradientDrawable();
        ring.setShape(GradientDrawable.OVAL);
        ring.setStroke(dp(2), BUTTON);
        help.setBackground(ring);
        help.setOnClickListener(v -> showControls());
        LayoutParams corner = new LayoutParams(dp(48), dp(48), Gravity.TOP | Gravity.END);
        addView(help, corner);
        // The screen reaches under the system bars as the splash did; the ? keeps clear
        setOnApplyWindowInsetsListener((v, insets) -> {
            Insets bars = insets.getInsets(WindowInsets.Type.systemBars() | WindowInsets.Type.displayCutout());
            corner.setMargins(bars.left + dp(12), bars.top + dp(12), bars.right + dp(12), bars.bottom + dp(12));
            help.setLayoutParams(corner);
            return insets;
        });
    }

    // The prompt starts below the logo
    @Override
    protected void onMeasure(int widthSpec, int heightSpec) {
        ((LayoutParams) prompt.getLayoutParams()).topMargin = (MeasureSpec.getSize(heightSpec) + logoSide) / 2 + dp(24);
        super.onMeasure(widthSpec, heightSpec);
    }

    /** The buttons the splash lacked appear as it leaves, the app having loaded meanwhile. */
    void fadeIn() {
        for (View v : new View[] {prompt, help}) {
            v.setAlpha(0);
            v.animate().alpha(1).setDuration(FADE_MS);
        }
    }

    /** The touch controls in a window nearly the screen's size: × closes it, as do Back and a tap outside. */
    private void showControls() {
        Context c = getContext();
        Dialog dialog = new Dialog(c, android.R.style.Theme_DeviceDefault_Dialog_NoActionBar);
        FrameLayout outside = new FrameLayout(c);
        outside.setOnClickListener(v -> dialog.dismiss());
        LinearLayout window = new LinearLayout(c);
        window.setOrientation(LinearLayout.VERTICAL);
        window.setClickable(true); // a tap inside is not one outside
        window.setBackground(frame(c));
        LayoutParams margin = new LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT);
        margin.setMargins(dp(16), dp(16), dp(16), dp(16));
        outside.addView(window, margin);

        FrameLayout head = new FrameLayout(c);
        TextView title = text("Touch controls", 18, Color.WHITE);
        title.setTypeface(MEDIUM);
        title.setPadding(dp(16), 0, dp(56), 0);
        head.addView(title, new LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT,
                Gravity.CENTER_VERTICAL | Gravity.START));
        TextView close = text("×", 28, Color.WHITE);
        close.setGravity(Gravity.CENTER);
        close.setContentDescription("Close");
        close.setOnClickListener(v -> dialog.dismiss());
        head.addView(close, new LayoutParams(dp(48), dp(48), Gravity.CENTER_VERTICAL | Gravity.END));
        window.addView(head, new LinearLayout.LayoutParams(LayoutParams.MATCH_PARENT, dp(56)));

        LinearLayout rows = new LinearLayout(c);
        rows.setOrientation(LinearLayout.VERTICAL);
        rows.setPadding(dp(16), dp(4), dp(16), dp(16));
        controls(rows, CONTROLS);
        TextView bar = text(BAR_TITLE, 12, ACCENT);
        bar.setTypeface(MEDIUM);
        bar.setAllCaps(true);
        bar.setLetterSpacing(0.08f);
        bar.setPadding(0, dp(8), 0, dp(4));
        rows.addView(bar);
        TextView where = text(BAR, 15, MUTED);
        where.setPadding(0, 0, 0, dp(14));
        rows.addView(where);
        controls(rows, BAR_CONTROLS);
        TextView keyboard = text(KEYBOARD, 15, MUTED);
        keyboard.setPadding(0, dp(8), 0, 0);
        rows.addView(keyboard);
        ScrollView scroll = new ScrollView(c);
        scroll.addView(rows);
        window.addView(scroll, new LinearLayout.LayoutParams(LayoutParams.MATCH_PARENT, 0, 1));

        dialog.setContentView(outside);
        dialog.getWindow().setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT);
        dialog.getWindow().setBackgroundDrawable(new ColorDrawable(Color.TRANSPARENT));
        // The ? sits nearer the edge than the window, and would show past its corner
        help.setVisibility(INVISIBLE);
        dialog.setOnDismissListener(d -> help.setVisibility(VISIBLE));
        dialog.show();
    }

    private void controls(LinearLayout rows, String[][] controls) {
        for (String[] control : controls) {
            TextView gesture = text(control[0], 15, Color.WHITE);
            gesture.setTypeface(MEDIUM);
            rows.addView(gesture);
            TextView does = text(control[1], 15, MUTED);
            does.setPadding(dp(16), dp(2), 0, dp(14));
            rows.addView(does);
        }
    }

    private TextView text(String s, float sp, int color) {
        return text(getContext(), s, sp, color);
    }

    /** A line of the site's text: Roboto at sp, in color. */
    static TextView text(Context c, String s, float sp, int color) {
        TextView t = new TextView(c);
        t.setText(s);
        t.setTextSize(TypedValue.COMPLEX_UNIT_SP, sp);
        t.setTextColor(color);
        t.setTypeface(Typeface.SANS_SERIF);
        return t;
    }

    /** A window's frame: the page inside a hairline, its corners rounded. */
    static GradientDrawable frame(Context c) {
        float density = c.getResources().getDisplayMetrics().density;
        GradientDrawable frame = new GradientDrawable();
        frame.setColor(c.getColor(R.color.page));
        frame.setStroke(Math.round(density), LINE);
        frame.setCornerRadius(8 * density);
        return frame;
    }

    private int dp(float v) {
        return Math.round(v * getResources().getDisplayMetrics().density);
    }
}
