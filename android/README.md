# vif for Android

The Android host of [Multi-platform §8](../doc/multi-platform.md#8-adding-another-renderer-or-android-host):
the game runs in-process through a `gomobile bind` library, presents on a
`SurfaceView` and plays through an `AudioTrack`. Nothing in the simulation knows.

| Path | What |
|---|---|
| `go.mod`, `game/` | The Go host, a module of its own so the repository's `go build ./...` never sees gomobile. `game.Start` builds an `app.Config` whose `Host` is the view and the speakers and calls `app.Run`. |
| `app/` | The Java app: `MainActivity` (lifecycle, assets, Back), `StartScreen` (what the app opens on), `GameView` (render thread, taps, keys), `CellDrawer` (`OnCanvas`, and `FromRaster`, the fallback), `Speaker`; `res/` holds `../lixen-logo.svg` as the vector `logo`, which the start screen shows, Android's splash screen insets to the start screen's size on its page (`themes.xml`) and the launcher icon insets on black, with its lines alone as the themed icon. |
| `../wad/` | Packaged as the APK's `assets/wad/` and copied to the app's files directory once per install, where the game reads it as its system root. |
| `files/config/vif/vif.toml` | The user root's settings, read and written back as a terminal run does: each `[play]` value (sfx, music, sfx volume, auto-fire, mouse, HUD) as it changes, the file created at the first change. No permission: the app's own directory. |
| `toolchain.sh` | What the build needs: `make android` checks it, and `go.mod` here against the root's, before Gradle runs; `make android-toolchain` installs it. |

## Build

Needs Go (the version in `go.mod`), a JDK (Gradle's wrapper starts on its `java`,
and gomobile compiles the binding with the `javac` on `PATH`), and an Android SDK
holding platform `android-36.1` and NDK `29.0.14206865`, its licence accepted.
The SDK is `sdk.dir=<path>` in `android/local.properties` (ignored by git), else
`ANDROID_HOME`, else Android Studio's default, `~/Android/Sdk`
(`~/Library/Android/sdk` on macOS). The first build downloads Gradle, the JDK 21
it builds on and the SDK build tools; later builds reuse them, and Gradle's build and
configuration caches (`gradle.properties`) rerun only what changed.

```bash
make android             # names every missing piece and its fix, else bin/vif-debug-arm64.apk and -x86_64.apk
make android-toolchain   # installs the missing pieces
make android-install     # adb install -r the APK of the device's ABI, adb from PATH or the SDK
```

`make android` reports the JDK and SDK pieces it lacks and installs none of them.
`make android-toolchain` offers the system's Go and JDK packages and installs each
on a yes; into the SDK it unpacks Google's command-line tools when it has no
`sdkmanager` (the build `toolchain.sh` pins, checked against its published
SHA-256), then has `sdkmanager` install the platform, NDK and `platform-tools`,
asking to accept their licence; it records `sdk.dir` when nothing named the SDK.
Google ships the SDK's tools for Linux, macOS and Windows only; on FreeBSD, build
the APK on one of those.

`bindGo` builds gomobile and gobind at the versions `go.mod` pins, binds `./game`
for arm64 and x86_64 into `app/build/gomobile/vif.aar`, and an APK for each lands
in `app/build/outputs/apk/debug/`: a phone takes arm64, an emulator x86_64. Its
library is compressed, so installing unpacks it once and each launch loads that
copy. Gradle packages a debug APK whole on every build, as it does a release
(`gradle.properties`), so rebuilding never grows it. This module requires at least
what the root's `go.mod` does, so a dependency bump there runs
`go -C android mod tidy` in the same commit; until it does, `make android` names
each module behind and what requires it. `make verify` vets and tests this
module's Go side with the rest.

## Nightly

The release workflow (`.github/workflows/release.yml`) builds
`vif-nightly-android-arm64.apk` and `vif-nightly-android-x86_64.apk` every night,
and `vif-<version>-android-<abi>.apk` on a tag, with
`android/toolchain.sh gradlew assembleRelease` on the runner's SDK: Android 11 and
newer, published under the repository's `nightly` release. Its `versionCode` is
the workflow's run number, so each build installs over the one before it, and it
is signed with `debug.keystore` here, the key `make android` signs with too, so a
nightly and a local build install over each other; the `ANDROID_KEYSTORE_BASE64`,
`ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD` secrets
put a release key in its place
([todo](../doc/todo.md#sign-the-android-build-with-a-release-key)).
`adb install -r vif-nightly-android-arm64.apk` installs it on a phone, as does the
phone's browser once it may install apps; an emulator takes the x86_64 one.

## Run

```bash
adb shell am start -n com.lixen.vif/.MainActivity
```

The app opens on its start screen, in lixen.com's dark theme: the logo at the
centre, "Tap to start" under it on the site's blue button, and a `?` in the corner
that opens the touch controls in a window nearly the screen's size, which its ×,
Back or a tap outside closes. Android's splash screen, up while the app loads, is
the same page with the logo 128 dp across in the same place, so when it goes only
the buttons appear, fading in. The run starts behind
it, paused at its first tick and silent, and a tap anywhere else lifts the screen
off the game; a screen turned first starts that game over at its new size. Back
there is the system's, and no key reaches the game until the tap.
A browser shows the same screen in the same words and waits for its tap too; a
desktop's asks for a click or a key instead, and a terminal or the desktop window
draws its own over the waiting game.

A cell is 12 dp tall (`cellDP`) until a pinch zooms it, and the grid the view
holds is stretched over the view edge to edge. Add `--ez raster true` to paint with
Go's software raster instead of the Canvas drawer. `adb logcat -s vif` prints the
render thread's frame time every five seconds: frames painted per second, mean, p95
and max of take, paint and post.

Controls: a tap is a left press and release on the cell under it, a finger moved
while down drags the cursor, and one held on the status bar opens the
configuration menu, whose rows a tap selects and a second tap acts on; keys, a
hardware keyboard's or the system keyboard's, arrive as a terminal reports them;
the keyboard's paste button, the Paste key, Ctrl-Shift-V and Shift-Insert type the
clipboard's text, a `:join` link copied from the lobby for one, however long;
three taps on the status bar, each lifted within 0.3 s and the next within half a
second, raise or drop the system keyboard, landing anywhere on the bar (the strip
above the keyboard stands for it) and each finger straying up to Android's double
tap slop, and so do two fingers tapped together twice within half a second (a
finger joining one held or dragging is play, not the gesture, and so is a single
pair tap); a pinch zooms the cell from 4 to 40 dp, the browser's range
too, never under the atlas's 12x24 pixels, the grid refitting as it does on a
rotation (a `crop_on_resize` map crops to it) and the cell kept in `vif.toml`'s
`[presentation] cell` for the next run; two taps near each other within
`MouseDoubleClickWindow` fire special; with mouse input disabled a press on the
status bar still opens the menu; Back is Escape; `:q` quits. The keyboard
covers the picture rather than resizing it, which on a `crop_on_resize` map would
delete what it covered; while it is up the left half of the status bar's bottom
row, its mode and input, is drawn again just above the keyboard at twice its size,
the input kept to that half so what is typed can be read; a rotation still resizes. Back is Escape, and a second
Back within a second and a half sends the app to the background, the game paused.

## Test

`go test ./...` here covers the tap and key mapping; the frame hand-off and that a
cell frame painted from the atlas texture is the raster's pixels are `pkg/glyph`'s
tests. On a device or
emulator, `./gradlew connectedDebugAndroidTest` checks `CellDrawer.OnCanvas`
against `glyph.Raster` pixel for pixel on Android's own Canvas. It runs on every
device adb sees and uninstalls the app from each afterwards, its files with it:
name one, as `ANDROID_SERIAL=emulator-5554`, when a phone you play on is plugged
in.

A headless emulator with `-gpu host` has crashed when an install killed the
running app; `-gpu swiftshader_indirect` does not, at a slower GPU.
