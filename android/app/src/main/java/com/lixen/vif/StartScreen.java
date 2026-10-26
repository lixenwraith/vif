package com.lixen.vif;

import android.app.Dialog;
import android.content.Context;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.ColorDrawable;
import android.graphics.drawable.GradientDrawable;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.ViewGroup;
import android.widget.FrameLayout;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

/**
 * The screen the app opens on, over the game: the logo at the centre, "Tap to start"
 * under it on a button and a ? in the corner that opens the touch controls. The run
 * starts behind it and waits, paused; a tap anywhere else lifts the screen and the
 * game goes on at once. A browser on a touch screen shows the same screen
 * (web/index.html) in the same words while it loads, and needs no tap. Both wear
 * lixen.com's dark theme in its font, Roboto, the system's sans-serif here.
 */
final class StartScreen extends FrameLayout {
    // The touch controls as web/index.html words them; Back is each platform's own
    private static final String[][] CONTROLS = {
            {"Tap", "Move the cursor there and fire the main weapon; hold to repeat"},
            {"Drag", "Drag the cursor with your finger"},
            {"Double tap", "Fire the special weapon as well"},
            {"Hold the status bar", "Open the configuration menu: audio, bots, multiplayer and more"},
            {"Two fingers, tap twice", "Show or hide the keyboard, to type commands such as :help"},
            {"Pinch", "Zoom in or out; the zoom is kept for next time"},
            {"Back", "Escape; twice quickly, leave with the game paused"},
    };
    private static final String KEYBOARD = "With a keyboard, F1 or :help lists every key and command, and :q quits.";
    // The site's page, which the window wears under the system bars too, then its
    // hairline, muted text, the primary button's blue and the periwinkle a ring of
    // that blue is labelled in, as web/terminal.css names them
    static final int BG = 0xFF11111B;
    private static final int LINE = 0x26FFFFFF, MUTED = 0xFF94A3B8, BUTTON = 0xFF2563EB, ACCENT = 0xFF8EA2FF;
    private static final Typeface MEDIUM = Typeface.create("sans-serif-medium", Typeface.NORMAL);

    private final ImageView logo;
    private final TextView prompt;
    private final TextView help;

    StartScreen(Context context, Runnable start) {
        super(context);
        setBackgroundColor(BG);
        setOnClickListener(v -> start.run());
        logo = new ImageView(context);
        logo.setImageResource(R.drawable.logo);
        logo.setContentDescription("lixen");
        addView(logo, new LayoutParams(0, 0, Gravity.CENTER));
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
        corner.setMargins(dp(12), dp(12), dp(12), dp(12));
        addView(help, corner);
    }

    // The logo takes two fifths of the shorter side, and the prompt starts below it
    @Override
    protected void onMeasure(int widthSpec, int heightSpec) {
        int h = MeasureSpec.getSize(heightSpec);
        int side = Math.min(MeasureSpec.getSize(widthSpec), h) * 2 / 5;
        logo.getLayoutParams().width = side;
        logo.getLayoutParams().height = side;
        ((LayoutParams) prompt.getLayoutParams()).topMargin = (h + side) / 2 + dp(24);
        super.onMeasure(widthSpec, heightSpec);
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
        GradientDrawable frame = new GradientDrawable();
        frame.setColor(BG);
        frame.setStroke(dp(1), LINE);
        frame.setCornerRadius(dp(8));
        window.setBackground(frame);
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
        for (String[] control : CONTROLS) {
            TextView gesture = text(control[0], 15, Color.WHITE);
            gesture.setTypeface(MEDIUM);
            rows.addView(gesture);
            TextView does = text(control[1], 15, MUTED);
            does.setPadding(dp(16), dp(2), 0, dp(14));
            rows.addView(does);
        }
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

    private TextView text(String s, float sp, int color) {
        TextView t = new TextView(getContext());
        t.setText(s);
        t.setTextSize(TypedValue.COMPLEX_UNIT_SP, sp);
        t.setTextColor(color);
        t.setTypeface(Typeface.SANS_SERIF);
        return t;
    }

    private int dp(float v) {
        return Math.round(v * getResources().getDisplayMetrics().density);
    }
}
