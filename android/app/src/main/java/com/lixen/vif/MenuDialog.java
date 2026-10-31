package com.lixen.vif;

import android.app.Dialog;
import android.content.Context;
import android.content.res.ColorStateList;
import android.graphics.Color;
import android.graphics.drawable.ColorDrawable;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.StateListDrawable;
import android.text.Editable;
import android.text.InputType;
import android.text.TextUtils;
import android.text.TextWatcher;
import android.util.Log;
import android.view.Gravity;
import android.view.KeyEvent;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.view.inputmethod.EditorInfo;
import android.view.inputmethod.InputMethodManager;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.RadioButton;
import android.widget.ScrollView;
import android.widget.Switch;
import android.widget.TextView;

import com.lixen.vif.game.Game;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * The configuration menu drawn with the platform's widgets from the run's snapshot
 * (doc/native-menu.md): a window nearly the screen's size in lixen.com's dark theme,
 * its rows 48 dp tall at least, each kind of node its own widget. A snapshot rewrites
 * the rows in place by id, so expansion, scroll and focus stay; a tap sends the run an
 * op, and the widget keeps the snapshot's value until the next says what happened.
 */
final class MenuDialog extends Dialog {
    // The contract with the run (internal/mode MenuKinds, MenuOps, MenuInputs), which a Go test holds this file to
    static final String[] KINDS = {"group", "toggle", "choice", "step", "pick", "action", "form", "info"};
    static final String[] OPS = {"set", "step", "activate", "submit", "search", "close"};
    static final String[] INPUTS = {"text", "number", "uri"};

    private static final int PRESS = 0x268EA2FF; // a row pressed or focused: the accent, faint
    private static final String NONE = "#none"; // the row shown when a search finds nothing

    // Which pages are expanded, kept for the run and not saved
    private static final Set<String> expanded = new HashSet<>();

    private final TextView title, status;
    private final EditText search;
    private final LinearLayout rows;
    private final Map<String, Row> byId = new HashMap<>();
    private String json = ""; // the snapshot applied, which an expansion applies again
    private String query = ""; // the search the run holds
    private Dialog sheet; // the confirm, picker or form up over the menu, if any
    private String sheetId; // the node it is up for
    private TextView sheetError; // a form sheet's error line

    MenuDialog(Context c) {
        super(c, android.R.style.Theme_DeviceDefault_Dialog_NoActionBar);
        FrameLayout outside = new FrameLayout(c);
        outside.setOnClickListener(v -> back());
        LinearLayout window = new LinearLayout(c);
        window.setOrientation(LinearLayout.VERTICAL);
        window.setClickable(true); // a tap inside is not one outside
        window.setBackground(StartScreen.frame(c));
        FrameLayout.LayoutParams margin = new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT);
        margin.setMargins(dp(16), dp(16), dp(16), dp(16));
        outside.addView(window, margin);

        FrameLayout head = new FrameLayout(c);
        title = StartScreen.text(c, "Menu", 18, Color.WHITE);
        title.setTypeface(StartScreen.MEDIUM);
        title.setSingleLine();
        title.setEllipsize(TextUtils.TruncateAt.END);
        title.setPadding(dp(16), 0, dp(56), 0);
        head.addView(title, new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT, Gravity.CENTER_VERTICAL | Gravity.START));
        TextView close = StartScreen.text(c, "×", 28, Color.WHITE);
        close.setGravity(Gravity.CENTER);
        close.setContentDescription("Close");
        close.setFocusable(true);
        close.setBackground(pressable());
        close.setOnClickListener(v -> Game.menu("close", "", ""));
        head.addView(close, new FrameLayout.LayoutParams(dp(48), dp(48), Gravity.CENTER_VERTICAL | Gravity.END));
        window.addView(head, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(56)));

        search = field(c, "text");
        search.setHint("Search the menu");
        search.setImeOptions(EditorInfo.IME_ACTION_SEARCH | EditorInfo.IME_FLAG_NO_EXTRACT_UI);
        search.setContentDescription("Search the menu");
        search.addTextChangedListener(new TextWatcher() {
            @Override
            public void beforeTextChanged(CharSequence s, int start, int count, int after) {
            }

            @Override
            public void onTextChanged(CharSequence s, int start, int before, int count) {
            }

            @Override
            public void afterTextChanged(Editable s) {
                Game.menu("search", "", s.toString());
            }
        });
        search.setOnEditorActionListener((v, action, e) -> {
            hideKeyboard(v); // the rows found stay
            return true;
        });
        LinearLayout.LayoutParams searchAt = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        searchAt.setMargins(dp(16), 0, dp(16), dp(8));
        window.addView(search, searchAt);

        rows = new LinearLayout(c);
        rows.setOrientation(LinearLayout.VERTICAL);
        rows.setPadding(0, 0, 0, dp(8));
        ScrollView scroll = new ScrollView(c);
        scroll.addView(rows);
        window.addView(scroll, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1));

        status = StartScreen.text(c, "", 14, Color.WHITE);
        status.setPadding(dp(16), dp(8), dp(16), dp(12));
        status.setVisibility(View.GONE);
        window.addView(status, new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        setContentView(outside);
        getWindow().setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT);
        getWindow().setBackgroundDrawable(new ColorDrawable(Color.TRANSPARENT));
        // This window resizes for the keyboard, which lifts a field above it, while the
        // activity's stays adjustNothing and the grid is never cropped
        getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE
                | WindowManager.LayoutParams.SOFT_INPUT_STATE_HIDDEN);
        setCanceledOnTouchOutside(false);
    }

    /** The menu as the run has it: the rows rewritten in place, keyed by id. */
    void apply(String json) {
        JSONObject snapshot;
        try {
            snapshot = new JSONObject(json);
        } catch (JSONException e) {
            Log.e(MainActivity.TAG, "menu snapshot", e);
            return;
        }
        this.json = json;
        title.setText(snapshot.optString("title"));
        JSONObject line = snapshot.optJSONObject("status");
        status.setVisibility(line == null ? View.GONE : View.VISIBLE);
        if (line != null) {
            status.setText(line.optString("text"));
            String kind = line.optString("kind");
            status.setTextColor(kind.equals("alert") ? StartScreen.DANGER : kind.equals("progress") ? StartScreen.ACCENT : Color.WHITE);
        }
        Map<String, JSONObject> nodes = new HashMap<>();
        index(snapshot.optJSONArray("nodes"), nodes);
        List<Entry> wanted = new ArrayList<>();
        query = snapshot.optString("query");
        if (!query.isEmpty()) {
            JSONArray found = snapshot.optJSONArray("found");
            for (int i = 0; found != null && i < found.length(); i++) {
                JSONObject f = found.optJSONObject(i);
                JSONObject node = f == null ? null : nodes.get(f.optString("id"));
                if (node != null) {
                    wanted.add(new Entry(node, f.optString("label"), 1));
                }
            }
            if (wanted.isEmpty()) {
                wanted.add(new Entry(new JSONObject(Map.of("id", NONE, "kind", "info", "label", "Nothing matches")), "Nothing matches", 1));
            }
        } else {
            walk(snapshot.optJSONArray("nodes"), 0, wanted);
        }
        reconcile(wanted);
        JSONObject form = snapshot.optJSONObject("form");
        String errorId = form == null ? "" : form.optString("id"), error = form == null ? "" : form.optString("error");
        for (Row row : byId.values()) {
            row.error(row.id.equals(errorId) ? error : "");
        }
        if (sheetError != null) {
            boolean shown = sheetId != null && sheetId.equals(errorId);
            sheetError.setText(shown ? error : "");
            sheetError.setVisibility(shown ? View.VISIBLE : View.GONE);
        }
        if (sheet != null && !nodes.containsKey(sheetId)) {
            sheet.dismiss(); // the row it was up for is gone: a bot that left
        }
    }

    private static void index(JSONArray nodes, Map<String, JSONObject> into) {
        for (int i = 0; nodes != null && i < nodes.length(); i++) {
            JSONObject n = nodes.optJSONObject(i);
            if (n != null) {
                into.put(n.optString("id"), n);
                index(n.optJSONArray("nodes"), into);
            }
        }
    }

    // walk lists the rows to show: every page, the rows of the expanded ones, and
    // a section as a title over its own rows
    private static void walk(JSONArray nodes, int depth, List<Entry> into) {
        for (int i = 0; nodes != null && i < nodes.length(); i++) {
            JSONObject n = nodes.optJSONObject(i);
            if (n == null) {
                continue;
            }
            into.add(new Entry(n, n.optString("label"), depth));
            if (n.optString("kind").equals("group") && (depth > 0 || expanded.contains(n.optString("id")))) {
                walk(n.optJSONArray("nodes"), depth + 1, into);
            }
        }
    }

    // reconcile makes the rows shown the entries wanted, in order, keeping every view
    // whose node stands so its state stays, and adding or removing where the tree changed
    private void reconcile(List<Entry> wanted) {
        for (int i = 0; i < wanted.size(); i++) {
            Entry e = wanted.get(i);
            Row row = byId.get(e.id);
            if (row != null && !(row.kind.equals(e.kind) && row.depth == e.depth)) {
                rows.removeView(row.view);
                byId.remove(e.id);
                row = null;
            }
            if (row == null) {
                row = new Row(e);
                byId.put(e.id, row);
                rows.addView(row.view, i);
            } else if (rows.indexOfChild(row.view) != i) {
                rows.removeView(row.view);
                rows.addView(row.view, i);
            }
            row.update(e);
        }
        while (rows.getChildCount() > wanted.size()) {
            View v = rows.getChildAt(wanted.size());
            rows.removeViewAt(wanted.size());
            byId.remove(((Row) v.getTag()).id);
        }
    }

    /** Back closes the innermost of confirm, picker and form, then clears a search, then sends close. */
    @Override
    public void onBackPressed() {
        back();
    }

    private void back() {
        if (sheet != null) {
            sheet.dismiss();
        } else if (search.length() > 0 || !query.isEmpty()) {
            search.setText("");
            if (query.isEmpty()) {
                Game.menu("search", "", "");
            }
        } else {
            Game.menu("close", "", "");
        }
    }

    /** Dismissed by the activity when the run says the menu closed, and whatever is up with it. */
    @Override
    public void dismiss() {
        if (sheet != null) {
            sheet.dismiss();
        }
        super.dismiss();
    }

    /**
     * Hardware keys, as in the tui menu, except in a field being typed in: j and k
     * move, h and l step or cycle the row focused, / finds the search field, q and
     * Escape go back, and Ctrl-G, F10 and F1 go to the run as keys, which close the
     * menu or open help over it.
     */
    @Override
    public boolean onKeyDown(int code, KeyEvent e) {
        View focus = getCurrentFocus();
        boolean typing = focus instanceof EditText;
        boolean plain = !e.isCtrlPressed() && !e.isAltPressed();
        if (code == KeyEvent.KEYCODE_F10 || code == KeyEvent.KEYCODE_F1 || code == KeyEvent.KEYCODE_G && e.isCtrlPressed()) {
            Game.key(code, GameView.typed(e), e.getMetaState());
            return true;
        }
        if (code == KeyEvent.KEYCODE_ESCAPE || code == KeyEvent.KEYCODE_Q && plain && !typing) {
            back();
            return true;
        }
        if (typing || !plain) {
            return super.onKeyDown(code, e);
        }
        Row row = rowOf(focus);
        switch (code) {
            case KeyEvent.KEYCODE_J:
                move(focus, View.FOCUS_DOWN);
                return true;
            case KeyEvent.KEYCODE_K:
                move(focus, View.FOCUS_UP);
                return true;
            case KeyEvent.KEYCODE_H:
                if (row != null) {
                    row.turn(-1);
                }
                return true;
            case KeyEvent.KEYCODE_L:
                if (row != null) {
                    row.turn(1);
                }
                return true;
            case KeyEvent.KEYCODE_SLASH:
                search.requestFocus();
                return true;
        }
        return super.onKeyDown(code, e);
    }

    private void move(View focus, int direction) {
        View next = focus == null ? rows.getChildCount() > 0 ? rows.getChildAt(0) : null : focus.focusSearch(direction);
        if (next != null) {
            next.requestFocus();
        }
    }

    // rowOf is the row a focused view belongs to, a widget inside one included
    private static Row rowOf(View v) {
        for (; v != null; v = v.getParent() instanceof View ? (View) v.getParent() : null) {
            if (v.getTag() instanceof Row) {
                return (Row) v.getTag();
            }
        }
        return null;
    }

    private void hideKeyboard(View v) {
        InputMethodManager m = getContext().getSystemService(InputMethodManager.class);
        if (m != null) {
            m.hideSoftInputFromWindow(v.getWindowToken(), 0);
        }
    }

    /** A node to show: its snapshot, the label to show it under and its depth in the tree. */
    private static final class Entry {
        final JSONObject node;
        final String id, kind, label;
        final int depth;

        Entry(JSONObject node, String label, int depth) {
            this.node = node;
            this.id = node.optString("id");
            this.kind = node.optString("kind");
            this.label = label;
            this.depth = depth;
        }
    }

    /**
     * One row: a label over its description, and the widget its kind takes, which
     * shows the snapshot's value and sends an op when touched. A page is a row with a
     * chevron that expands its rows under it; a section is a title.
     */
    private final class Row {
        final String id, kind;
        final int depth;
        final LinearLayout view;
        private final TextView label, detail;
        private TextView value, chevron, error;
        private Switch toggle;
        private RadioButton radio;
        private EditText field;
        private String[] choices = new String[0];
        private String[] fields = new String[0];
        private boolean disabled;

        Row(Entry e) {
            id = e.id;
            kind = e.kind;
            depth = e.depth;
            Context c = getContext();
            view = new LinearLayout(c);
            view.setTag(this);
            view.setOrientation(LinearLayout.HORIZONTAL);
            view.setGravity(Gravity.CENTER_VERTICAL);
            view.setMinimumHeight(dp(48));
            view.setPadding(dp(16), dp(10), dp(16), dp(10));
            boolean section = kind.equals("group") && depth > 0;
            label = StartScreen.text(c, e.label, section ? 13 : 16, Color.WHITE);
            detail = StartScreen.text(c, "", 14, StartScreen.MUTED);
            LinearLayout column = new LinearLayout(c);
            column.setOrientation(LinearLayout.VERTICAL);
            column.addView(label);
            column.addView(detail);
            switch (kind) {
                case "group":
                    label.setTypeface(StartScreen.MEDIUM);
                    if (section) {
                        label.setTextColor(StartScreen.ACCENT);
                        label.setLetterSpacing(0.06f);
                        label.setAllCaps(true);
                        view.setPadding(dp(16), dp(18), dp(16), dp(2));
                        view.setMinimumHeight(0);
                        detail.setVisibility(View.GONE);
                    } else {
                        chevron = StartScreen.text(c, "", 16, StartScreen.ACCENT);
                        chevron.setPadding(dp(12), 0, 0, 0);
                        actable(c);
                    }
                    break;
                case "toggle":
                    toggle = new Switch(c);
                    toggle.setClickable(false);
                    toggle.setFocusable(false);
                    toggle.setThumbTintList(checked(StartScreen.BUTTON, StartScreen.MUTED));
                    toggle.setTrackTintList(checked(0x802563EB, StartScreen.LINE));
                    toggle.setPadding(dp(12), 0, 0, 0);
                    actable(c);
                    break;
                case "choice":
                    value = StartScreen.text(c, "", 16, StartScreen.ACCENT);
                    value.setPadding(dp(12), 0, 0, 0);
                    actable(c);
                    break;
                case "step":
                    value = StartScreen.text(c, "", 16, Color.WHITE);
                    value.setGravity(Gravity.CENTER);
                    value.setMinWidth(dp(56));
                    view.setFocusable(true);
                    view.setBackground(pressable());
                    break;
                case "pick":
                    radio = new RadioButton(c);
                    radio.setClickable(false);
                    radio.setFocusable(false);
                    radio.setButtonTintList(checked(StartScreen.BUTTON, StartScreen.MUTED));
                    value = StartScreen.text(c, "", 14, StartScreen.MUTED);
                    value.setPadding(dp(12), 0, 0, 0);
                    actable(c);
                    break;
                case "action":
                    label.setTextColor(StartScreen.ACCENT);
                    label.setTypeface(StartScreen.MEDIUM);
                    actable(c);
                    break;
                case "form":
                    label.setTypeface(StartScreen.MEDIUM);
                    fields(e.node);
                    if (fields.length == 1) {
                        field = field(c, inputOf(e.node, 0));
                        field.setText(e.node.optJSONArray("fields").optJSONObject(0).optString("value"));
                        field.setHint(fields[0]);
                        field.setImeOptions(EditorInfo.IME_ACTION_GO | EditorInfo.IME_FLAG_NO_EXTRACT_UI);
                        field.setOnEditorActionListener((v, action, ev) -> {
                            submit(new String[] {field.getText().toString()});
                            return true;
                        });
                        error = StartScreen.text(c, "", 14, StartScreen.DANGER);
                        error.setVisibility(View.GONE);
                        LinearLayout.LayoutParams at = new LinearLayout.LayoutParams(
                                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
                        at.setMargins(0, dp(6), 0, dp(4));
                        column.addView(field, 1, at);
                        column.addView(error, 2);
                    } else {
                        label.setTextColor(StartScreen.ACCENT);
                        actable(c);
                    }
                    break;
                default:
                    break;
            }
            if (radio != null) {
                radio.setPadding(0, 0, dp(12), 0);
                view.addView(radio);
            }
            view.addView(column, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
            if (kind.equals("step")) {
                view.addView(stepper("−", -1));
                view.addView(value);
                view.addView(stepper("+", 1));
            } else {
                for (View w : new View[] {value, toggle, chevron}) {
                    if (w != null) {
                        view.addView(w);
                    }
                }
            }
        }

        // actable makes the whole row the touch target, which a tap acts on and a key focuses
        private void actable(Context c) {
            view.setClickable(true);
            view.setFocusable(true);
            view.setBackground(pressable());
            view.setOnClickListener(v -> act());
        }

        private View stepper(String sign, int direction) {
            TextView b = StartScreen.text(getContext(), sign, 20, StartScreen.ACCENT);
            b.setGravity(Gravity.CENTER);
            b.setContentDescription(direction < 0 ? "Less" : "More");
            b.setBackground(ring(getContext()));
            b.setClickable(true);
            b.setOnClickListener(v -> turn(direction));
            LinearLayout.LayoutParams at = new LinearLayout.LayoutParams(dp(40), dp(40));
            at.setMargins(dp(8), 0, dp(8), 0);
            b.setLayoutParams(at);
            return b;
        }

        private void fields(JSONObject node) {
            JSONArray fs = node.optJSONArray("fields");
            fields = new String[fs == null ? 0 : fs.length()];
            for (int i = 0; i < fields.length; i++) {
                fields[i] = fs.optJSONObject(i).optString("label");
            }
        }

        /** Rewrites the row from a snapshot's node; what was typed into a field stays. */
        void update(Entry e) {
            JSONObject n = e.node;
            label.setText(e.label);
            String reason = n.optString("disabled"), description = n.optString("description");
            disabled = !reason.isEmpty();
            String text = disabled && !description.isEmpty() ? reason + ". " + description : disabled ? reason : description;
            detail.setText(text);
            detail.setVisibility(text.isEmpty() || kind.equals("group") && depth > 0 ? View.GONE : View.VISIBLE);
            view.setAlpha(disabled ? 0.5f : 1);
            JSONArray cs = n.optJSONArray("choices");
            choices = new String[cs == null ? 0 : cs.length()];
            for (int i = 0; i < choices.length; i++) {
                choices[i] = cs.optString(i);
            }
            String shown = n.optString("value");
            switch (kind) {
                case "group":
                    if (chevron != null) {
                        boolean open = expanded.contains(id);
                        chevron.setText(open ? "▾" : "▸");
                        view.setContentDescription(e.label + (open ? ", expanded" : ", collapsed"));
                    }
                    return;
                case "toggle":
                    toggle.setChecked(choices.length == 2 && shown.equals(choices[1]));
                    break;
                case "choice":
                    value.setText(shown + " ▾");
                    break;
                case "step":
                    value.setText(shown);
                    break;
                case "pick":
                    radio.setChecked(n.optBoolean("marked"));
                    value.setText(shown);
                    value.setVisibility(shown.isEmpty() ? View.GONE : View.VISIBLE);
                    break;
                default:
                    break;
            }
            if (field != null) {
                field.setEnabled(!disabled);
            }
            String said = e.label;
            if (!shown.isEmpty() && !kind.equals("form")) {
                said += ", " + shown;
            }
            view.setContentDescription(disabled ? said + ", unavailable: " + reason : said);
        }

        /** The error a submit of this form left, shown under its field; none clears it. */
        void error(String text) {
            if (error != null) {
                error.setText(text);
                error.setVisibility(text.isEmpty() ? View.GONE : View.VISIBLE);
            }
        }

        // act is a tap on the row or Enter on it
        private void act() {
            switch (kind) {
                case "group":
                    if (!expanded.remove(id)) {
                        expanded.add(id);
                    }
                    apply(json);
                    break;
                case "toggle":
                    if (!disabled && choices.length == 2) {
                        Game.menu("set", id, toggle.isChecked() ? choices[0] : choices[1]);
                    }
                    break;
                case "choice":
                    if (!disabled) {
                        picker();
                    }
                    break;
                case "pick":
                    if (!disabled) {
                        Game.menu("activate", id, "");
                    }
                    break;
                case "action":
                    if (disabled) {
                        break;
                    }
                    if (byNode().optBoolean("confirm")) {
                        confirm();
                    } else {
                        Game.menu("activate", id, "");
                    }
                    break;
                case "form":
                    if (disabled) {
                        break;
                    }
                    if (field != null) {
                        field.requestFocus();
                    } else {
                        form();
                    }
                    break;
                default:
                    break;
            }
        }

        // turn is h or l, or − and +: a step moves, a toggle turns, a choice cycles
        void turn(int direction) {
            if (disabled) {
                return;
            }
            switch (kind) {
                case "step":
                    Game.menu("step", id, direction > 0 ? "+1" : "-1");
                    break;
                case "toggle":
                    if (choices.length == 2) {
                        Game.menu("set", id, choices[direction > 0 ? 1 : 0]);
                    }
                    break;
                case "choice":
                    int i = indexOf(choices, byNode().optString("value"));
                    if (choices.length > 0) {
                        Game.menu("set", id, choices[((i + direction) % choices.length + choices.length) % choices.length]);
                    }
                    break;
                case "pick":
                    act();
                    break;
                default:
                    break;
            }
        }

        // byNode is this row's node in the snapshot applied last
        private JSONObject byNode() {
            try {
                Map<String, JSONObject> nodes = new HashMap<>();
                index(new JSONObject(json).optJSONArray("nodes"), nodes);
                JSONObject n = nodes.get(id);
                return n == null ? new JSONObject() : n;
            } catch (JSONException e) {
                return new JSONObject();
            }
        }

        private void submit(String[] values) {
            Game.menu("submit", id, new JSONArray(List.of(values)).toString());
        }

        /** A radio list of the choices, in a window; the one tapped is set. */
        private void picker() {
            JSONObject n = byNode();
            Context c = getContext();
            LinearLayout body = sheet(c, label.getText().toString());
            TextView about = StartScreen.text(c, n.optString("description"), 14, StartScreen.MUTED);
            about.setPadding(0, 0, 0, dp(8));
            body.addView(about);
            String current = n.optString("value");
            for (String choice : choices) {
                RadioButton b = new RadioButton(c);
                b.setText(choice);
                b.setTextSize(16);
                b.setTextColor(Color.WHITE);
                b.setButtonTintList(checked(StartScreen.BUTTON, StartScreen.MUTED));
                b.setChecked(choice.equals(current));
                b.setMinHeight(dp(48));
                b.setOnClickListener(v -> {
                    Game.menu("set", id, choice);
                    sheet.dismiss();
                });
                body.addView(b);
            }
            sheet.show();
        }

        /** The question before a destructive action: its description, Cancel and the action. */
        private void confirm() {
            JSONObject n = byNode();
            Context c = getContext();
            LinearLayout body = sheet(c, label.getText().toString());
            TextView about = StartScreen.text(c, n.optString("description"), 15, StartScreen.MUTED);
            about.setPadding(0, 0, 0, dp(16));
            body.addView(about);
            body.addView(buttons(c, label.getText().toString(), v -> {
                Game.menu("activate", id, "");
                sheet.dismiss();
            }));
            sheet.show();
        }

        /** The fields of a longer form in a window, the focused one's help under it, the error above Submit. */
        private void form() {
            JSONObject n = byNode();
            Context c = getContext();
            LinearLayout body = sheet(c, label.getText().toString());
            TextView about = StartScreen.text(c, n.optString("description"), 14, StartScreen.MUTED);
            about.setPadding(0, 0, 0, dp(12));
            body.addView(about);
            JSONArray fs = n.optJSONArray("fields");
            EditText[] inputs = new EditText[fs == null ? 0 : fs.length()];
            for (int i = 0; i < inputs.length; i++) {
                JSONObject f = fs.optJSONObject(i);
                TextView name = StartScreen.text(c, f.optString("label"), 14, StartScreen.MUTED);
                name.setPadding(0, dp(8), 0, dp(4));
                body.addView(name);
                EditText input = field(c, f.optString("input"));
                input.setText(f.optString("value"));
                input.setImeOptions((i == inputs.length - 1 ? EditorInfo.IME_ACTION_DONE : EditorInfo.IME_ACTION_NEXT) | EditorInfo.IME_FLAG_NO_EXTRACT_UI);
                body.addView(input);
                TextView help = StartScreen.text(c, f.optString("help"), 14, StartScreen.MUTED);
                help.setPadding(0, dp(4), 0, 0);
                help.setVisibility(View.GONE);
                body.addView(help);
                input.setOnFocusChangeListener((v, focused) -> help.setVisibility(focused && help.length() > 0 ? View.VISIBLE : View.GONE));
                inputs[i] = input;
            }
            sheetError = StartScreen.text(c, "", 14, StartScreen.DANGER);
            sheetError.setPadding(0, dp(12), 0, 0);
            sheetError.setVisibility(View.GONE);
            body.addView(sheetError);
            View.OnClickListener submit = v -> {
                String[] values = new String[inputs.length];
                for (int i = 0; i < inputs.length; i++) {
                    values[i] = inputs[i].getText().toString();
                }
                submit(values);
            };
            if (inputs.length > 0) {
                inputs[inputs.length - 1].setOnEditorActionListener((v, action, ev) -> {
                    submit.onClick(v);
                    return true;
                });
            }
            View row = buttons(c, "Submit", submit);
            row.setPadding(0, dp(16), 0, 0);
            body.addView(row);
            sheet.getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE
                    | WindowManager.LayoutParams.SOFT_INPUT_STATE_VISIBLE);
            sheet.show();
            if (inputs.length > 0) {
                inputs[0].requestFocus();
            }
        }

        // sheet opens the window the picker, the confirm and a form share, titled, and
        // returns the column its content goes in; dismissing it forgets it
        private LinearLayout sheet(Context c, String heading) {
            if (sheet != null) {
                sheet.dismiss();
            }
            Dialog d = new Dialog(c, android.R.style.Theme_DeviceDefault_Dialog_NoActionBar);
            LinearLayout body = new LinearLayout(c);
            body.setOrientation(LinearLayout.VERTICAL);
            body.setBackground(StartScreen.frame(c));
            body.setPadding(dp(20), dp(16), dp(20), dp(16));
            TextView head = StartScreen.text(c, heading, 18, Color.WHITE);
            head.setTypeface(StartScreen.MEDIUM);
            head.setPadding(0, 0, 0, dp(8));
            body.addView(head);
            ScrollView scroll = new ScrollView(c);
            scroll.addView(body);
            d.setContentView(scroll);
            d.getWindow().setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
            d.getWindow().setBackgroundDrawable(new ColorDrawable(Color.TRANSPARENT));
            d.setOnDismissListener(x -> {
                if (sheet == d) {
                    sheet = null;
                    sheetId = null;
                    sheetError = null;
                }
            });
            sheet = d;
            sheetId = id;
            return body;
        }
    }

    // buttons is Cancel beside the action named, the site's ring and fill
    private View buttons(Context c, String action, View.OnClickListener onAction) {
        LinearLayout row = new LinearLayout(c);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.END);
        TextView cancel = button(c, "Cancel", false);
        cancel.setOnClickListener(v -> sheet.dismiss());
        TextView go = button(c, action, true);
        go.setOnClickListener(onAction);
        LinearLayout.LayoutParams gap = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        gap.setMargins(dp(12), 0, 0, 0);
        row.addView(cancel);
        row.addView(go, gap);
        return row;
    }

    // button is the site's: a blue fill under a white label, or a blue ring around a
    // periwinkle one, either filling royal blue while pressed
    private TextView button(Context c, String text, boolean fill) {
        TextView b = StartScreen.text(c, text, 16, fill ? Color.WHITE : StartScreen.ACCENT);
        b.setTypeface(StartScreen.MEDIUM);
        b.setGravity(Gravity.CENTER);
        b.setMinHeight(dp(44));
        b.setMinWidth(dp(96));
        b.setPadding(dp(20), dp(10), dp(20), dp(10));
        GradientDrawable rest = fill ? filled(StartScreen.BUTTON) : ring(c);
        GradientDrawable pressed = filled(StartScreen.HOVER);
        StateListDrawable states = new StateListDrawable();
        states.addState(new int[] {android.R.attr.state_pressed}, pressed);
        states.addState(new int[] {android.R.attr.state_focused}, pressed);
        states.addState(new int[] {}, rest);
        b.setBackground(states);
        b.setTextColor(new ColorStateList(new int[][] {{android.R.attr.state_pressed}, {android.R.attr.state_focused}, {}},
                new int[] {Color.WHITE, Color.WHITE, fill ? Color.WHITE : StartScreen.ACCENT}));
        b.setClickable(true);
        b.setFocusable(true);
        return b;
    }

    private GradientDrawable filled(int color) {
        GradientDrawable d = new GradientDrawable();
        d.setColor(color);
        d.setCornerRadius(dp(8));
        return d;
    }

    private GradientDrawable ring(Context c) {
        GradientDrawable d = new GradientDrawable();
        d.setColor(Color.TRANSPARENT);
        d.setStroke(dp(2), StartScreen.BUTTON);
        d.setCornerRadius(dp(8));
        return d;
    }

    // field is the site's text field: the surface colour inside a hairline, the ring's
    // blue while focused; input picks the keyboard
    private EditText field(Context c, String input) {
        EditText f = new EditText(c);
        f.setTextSize(16);
        f.setTextColor(Color.WHITE);
        f.setHintTextColor(StartScreen.MUTED);
        f.setSingleLine();
        f.setPadding(dp(12), dp(10), dp(12), dp(10));
        int type = InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS;
        if (input.equals("number")) {
            type = InputType.TYPE_CLASS_NUMBER;
        } else if (input.equals("uri")) {
            type |= InputType.TYPE_TEXT_VARIATION_URI;
        }
        f.setInputType(type);
        GradientDrawable rest = new GradientDrawable();
        rest.setColor(StartScreen.SURFACE);
        rest.setStroke(dp(1), StartScreen.LINE);
        rest.setCornerRadius(dp(8));
        GradientDrawable focused = new GradientDrawable();
        focused.setColor(StartScreen.SURFACE);
        focused.setStroke(dp(1), StartScreen.BUTTON);
        focused.setCornerRadius(dp(8));
        StateListDrawable states = new StateListDrawable();
        states.addState(new int[] {android.R.attr.state_focused}, focused);
        states.addState(new int[] {}, rest);
        f.setBackground(states);
        return f;
    }

    private static ColorStateList checked(int on, int off) {
        return new ColorStateList(new int[][] {{android.R.attr.state_checked}, {}}, new int[] {on, off});
    }

    // pressable tints a row while it is pressed or focused and leaves it clear otherwise
    private static StateListDrawable pressable() {
        StateListDrawable d = new StateListDrawable();
        d.addState(new int[] {android.R.attr.state_pressed}, new ColorDrawable(PRESS));
        d.addState(new int[] {android.R.attr.state_focused}, new ColorDrawable(PRESS));
        d.addState(new int[] {}, new ColorDrawable(Color.TRANSPARENT));
        return d;
    }

    private static String inputOf(JSONObject node, int i) {
        JSONArray fs = node.optJSONArray("fields");
        JSONObject f = fs == null ? null : fs.optJSONObject(i);
        return f == null ? "text" : f.optString("input");
    }

    private static int indexOf(String[] a, String s) {
        for (int i = 0; i < a.length; i++) {
            if (a[i].equals(s)) {
                return i;
            }
        }
        return -1;
    }

    private int dp(float v) {
        return Math.round(v * getContext().getResources().getDisplayMetrics().density);
    }
}
