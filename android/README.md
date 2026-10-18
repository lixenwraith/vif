# vif for Android

The Android host of [Multi-platform §8](../doc/multi-platform.md#8-adding-another-renderer-or-android-host):
the game runs in-process through a `gomobile bind` library, presents on a
`SurfaceView` and plays through an `AudioTrack`. Nothing in the simulation knows.

| Path | What |
|---|---|
| `go.mod`, `game/` | The Go host, a module of its own so the repository's `go build ./...` never sees gomobile. `game.Start` builds an `app.Config` whose `Host` is the view and the speakers and calls `app.Run`. |
| `app/` | The Java app: `MainActivity` (lifecycle, assets, Back), `GameView` (render thread, taps, keys), `CellDrawer` (`OnCanvas`, and `FromRaster`, the fallback), `Speaker`. |
| `../wad/` | Packaged as the APK's `assets/wad/` and copied to the app's files directory once per install, where the game reads it as its system root. |
| `files/config/vif/vif.toml` | The user root's settings, read as a terminal reads `vif.toml` and written as `[play]` changes (sound, volume, auto-fire, mouse, HUD); created at the first change, with sound on, where a terminal starts muted. No permission: the app's own directory. |
| `toolchain.sh` | What the build needs beside Go: `make android` checks it before Gradle runs, `make android-toolchain` installs it. |

## Build

Needs Go (the version in `go.mod`), a JDK (Gradle's wrapper starts on its `java`,
and gomobile compiles the binding with the `javac` on `PATH`), and an Android SDK
holding platform `android-36.1` and NDK `29.0.14206865`, its licence accepted.
The SDK is `sdk.dir=<path>` in `android/local.properties` (ignored by git), else
`ANDROID_HOME`, else Android Studio's default, `~/Android/Sdk`
(`~/Library/Android/sdk` on macOS). The first build downloads Gradle, the JDK 21
it builds on and the SDK build tools.

```bash
make android             # names every missing piece and its fix, else bin/vif-debug.apk
make android-toolchain   # installs the missing pieces
make android-install     # adb install -r bin/vif-debug.apk, adb from PATH or the SDK
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
for arm64 and x86_64 into `app/build/gomobile/vif.aar`, and the APK lands in
`app/build/outputs/apk/debug/`. A dependency bump at the repository root needs
`go mod tidy` here too, since this module requires at least what the root does.
`make verify` vets and tests this module's Go side with the rest.

## Run

```bash
adb shell am start -n com.lixen.vif/.MainActivity
```

A cell is 12 dp tall (`cellDP`), and the grid the view holds is stretched over the
view edge to edge. Add `--ez raster true` to paint with Go's software raster instead
of the Canvas drawer. `adb logcat -s vif` prints the render thread's frame time every five
seconds: frames painted per second, mean, p95 and max of take, paint and post.

Controls: a tap is a left press and release on the cell under it, a finger moved
while down drags the cursor, and one held on the status bar opens the
configuration menu, whose rows a tap selects and a second tap acts on; keys, a
hardware keyboard's or the system keyboard's, arrive as a terminal reports them;
two fingers tapped together raise or drop the system keyboard (a finger joining one
held or dragging is play, not the gesture); Back is Escape; `:q` quits. The keyboard
covers the picture rather than resizing it, which on a `crop_on_resize` map would
delete what it covered, and the status row is drawn again, twice its size, just
above it, so what is typed can be read; a rotation still resizes.

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
