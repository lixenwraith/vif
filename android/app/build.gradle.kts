plugins {
    id("com.android.application")
}

// The Go side is bound by gomobile from the module beside this project, which the
// repository root holds; wad/ is packaged as the APK's assets under wad/.
val goModule: File = rootDir
val repoRoot: File = rootDir.parentFile
val goBin = layout.buildDirectory.dir("gobin")
val boundAar = layout.buildDirectory.file("gomobile/vif.aar")

android {
    namespace = "com.lixen.vif"
    compileSdkVersion("android-36.1")
    ndkVersion = "29.0.14206865"

    defaultConfig {
        applicationId = "com.lixen.vif"
        minSdk = 30
        targetSdk = 36
        // The release workflow names each build; a local build is 1, "0.1".
        versionCode = (findProperty("versionCode") as String?)?.toInt() ?: 1
        versionName = findProperty("versionName") as String? ?: "0.1"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    // The repository's debug key, so a local build and the nightly install over each
    // other; a keystore the environment names signs a release (README.md).
    signingConfigs {
        getByName("debug") {
            storeFile = rootProject.file("debug.keystore")
            storePassword = "android"
            keyAlias = "androiddebugkey"
            keyPassword = "android"
        }
        create("release") {
            System.getenv("VIF_KEYSTORE")?.let {
                storeFile = file(it)
                storePassword = System.getenv("VIF_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("VIF_KEY_ALIAS")
                keyPassword = System.getenv("VIF_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        getByName("release") {
            signingConfig = signingConfigs.getByName(if (System.getenv("VIF_KEYSTORE") != null) "release" else "debug")
        }
    }

    // One APK per ABI bindGo builds, each library compressed: a phone downloads its
    // own alone, and installing unpacks it once (README.md).
    splits {
        abi {
            isEnable = true
            reset()
            include("arm64-v8a", "x86_64")
            isUniversalApk = false
        }
    }
    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    sourceSets["main"].assets.srcDir(layout.buildDirectory.dir("generated/wad").get().asFile)
}

val gomobileTools = tasks.register<Exec>("gomobileTools") {
    workingDir = goModule
    outputs.dir(goBin)
    commandLine("go", "build", "-o", goBin.get().asFile.path + "/",
        "golang.org/x/mobile/cmd/gomobile", "golang.org/x/mobile/cmd/gobind")
}

val sdk = androidComponents.sdkComponents
val bindGo = tasks.register<Exec>("bindGo") {
    dependsOn(gomobileTools)
    workingDir = goModule
    inputs.files(fileTree(repoRoot) { include("go.mod", "go.sum", "internal/**", "pkg/**") })
    inputs.files(fileTree(goModule) { include("go.mod", "go.sum", "game/**") })
    outputs.file(boundAar)
    // Locals, not the script's values: the configuration cache cannot keep an action
    // that holds the script itself
    val bin = goBin.get().asFile.path
    val sdkDir = sdk.sdkDirectory
    val ndkDir = sdk.ndkDirectory
    val aar = boundAar.get().asFile
    doFirst {
        environment("PATH", bin + File.pathSeparator + System.getenv("PATH"))
        environment("ANDROID_HOME", sdkDir.get().asFile.path)
        environment("ANDROID_NDK_HOME", ndkDir.get().asFile.path)
        aar.parentFile.mkdirs()
    }
    commandLine("$bin/gomobile", "bind", "-target=android/arm64,android/amd64", "-androidapi", "30",
        "-javapkg=com.lixen.vif", "-ldflags=-s -w", "-o", boundAar.get().asFile.path, "./game")
}

val wadAssets = tasks.register<Sync>("wadAssets") {
    from(repoRoot.resolve("wad"))
    into(layout.buildDirectory.dir("generated/wad/wad"))
}
tasks.named("preBuild") { dependsOn(wadAssets) }

dependencies {
    implementation(files(boundAar).builtBy(bindGo))
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
}
