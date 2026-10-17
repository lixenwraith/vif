# vif for Android (pilot)

The Android host of [Multi-platform §8](../doc/multi-platform.md#8-adding-another-renderer-or-android-host):
the game runs in-process through a `gomobile bind` library, presents on a
`SurfaceView` and plays through an `AudioTrack`. Nothing in the simulation knows.

| Path | What |
|---|---|
| `go.mod`, `game/` | The Go host, a module of its own so the repository's `go build ./...` never sees gomobile. `game.Start` builds an `app.Config` whose `Host` is the view and the speakers and calls `app.Run`. |
| `app/` | The Java app: `MainActivity` (lifecycle, assets, Back), `GameView` (render thread, taps, keys), `CellDrawer` (`OnCanvas`, and `FromRaster`, the fallback), `Speaker`. |
| `../wad/` | Packaged as the APK's `assets/wad/` and copied to the app's files directory once per install, where the game reads it as its system root. |

## Build

Needs Go (the version in `go.mod`), the Android SDK with platform `android-36.1`
and NDK `29.0.14206865` (`sdkmanager "ndk;29.0.14206865"`), and the SDK named by
`ANDROID_HOME` or by `sdk.dir=<path>` in `android/local.properties` (ignored by
git; Android Studio's default is `~/Android/Sdk`). Gradle provisions its own JDK 21.

```bash
make android           # cd android && ./gradlew assembleDebug, then bin/vif-debug.apk
make android-install   # adb install -r bin/vif-debug.apk
```

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

Controls: a tap is a left press and release on the cell under it, and one held on
the status bar opens the configuration menu, whose rows a tap selects and a second
tap acts on; keys, a hardware keyboard's or the
system keyboard's, arrive as a terminal reports them; a two-finger tap raises or
drops the system keyboard; Back is Escape; `:q` quits.

## Test

`go test ./...` here covers the tap and key mapping, the frame hand-off and that a
cell frame painted from the atlas texture is the raster's pixels. On a device or
emulator, `./gradlew connectedDebugAndroidTest` checks `CellDrawer.OnCanvas`
against `glyph.Raster` pixel for pixel on Android's own Canvas.

A headless emulator with `-gpu host` has crashed when an install killed the
running app; `-gpu swiftshader_indirect` does not, at a slower GPU.
